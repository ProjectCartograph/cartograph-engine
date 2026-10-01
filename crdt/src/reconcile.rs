//! Reconciling a whole JSON document into an Automerge document.
//!
//! A write that is not an edit (a save of the whole manifest, an import,
//! a file changed on disk) arrives as the full document. Replacing the
//! CRDT's content with it would give every field a new identity, so a
//! concurrent edit elsewhere would land on an object nobody sees any more.
//! Instead the document is diffed against what the CRDT holds and only
//! the differences are written: objects key by key, identified list items
//! by their key, sentences by character splices.

use std::collections::HashMap;

use automerge::transaction::Transactable;
use automerge::{AutoCommit, AutomergeError, ObjId, ObjType, Prop, ReadDoc, ScalarValue, Value};
use serde_json::{Map, Value as Json};

/// How a document's JSON maps onto Automerge types: which lists have
/// identified items, and which strings are texts. Pointers are split into
/// segments, and a "*" segment matches any segment.
pub struct Shape {
    list_keys: Vec<(Vec<String>, String)>,
    texts: Vec<Vec<String>>,
}

impl Shape {
    /// Reads `{"listKeys": {pointer: field}, "texts": [pointer]}`; both
    /// members are optional.
    pub fn parse(bytes: &[u8]) -> Result<Shape, String> {
        let mut shape = Shape {
            list_keys: Vec::new(),
            texts: Vec::new(),
        };
        if bytes.is_empty() {
            return Ok(shape);
        }
        let v: Json = serde_json::from_slice(bytes).map_err(|e| format!("shape: {e}"))?;
        if let Some(keys) = v.get("listKeys").and_then(Json::as_object) {
            for (pointer, field) in keys {
                let field = field
                    .as_str()
                    .ok_or_else(|| format!("shape: list key for {pointer} is not a string"))?;
                shape.list_keys.push((segments(pointer), field.to_string()));
            }
        }
        if let Some(texts) = v.get("texts").and_then(Json::as_array) {
            for pointer in texts {
                let pointer = pointer
                    .as_str()
                    .ok_or("shape: a text pointer is not a string")?;
                shape.texts.push(segments(pointer));
            }
        }
        Ok(shape)
    }

    fn list_key(&self, path: &[String]) -> Option<&str> {
        self.list_keys
            .iter()
            .find(|(p, _)| matches(p, path))
            .map(|(_, k)| k.as_str())
    }

    fn is_text(&self, path: &[String]) -> bool {
        self.texts.iter().any(|p| matches(p, path))
    }
}

/// Splits a JSON pointer into unescaped segments; "" is the root.
fn segments(pointer: &str) -> Vec<String> {
    pointer
        .split('/')
        .skip(1)
        .map(|s| s.replace("~1", "/").replace("~0", "~"))
        .collect()
}

fn matches(pattern: &[String], path: &[String]) -> bool {
    pattern.len() == path.len() && pattern.iter().zip(path).all(|(p, s)| p == "*" || p == s)
}

/// Writes the differences between `root` and the document into the open
/// transaction. The caller commits, or rolls back on error.
pub fn reconcile(
    doc: &mut AutoCommit,
    root: &Map<String, Json>,
    shape: &Shape,
) -> Result<(), AutomergeError> {
    Reconciler {
        doc,
        shape,
        path: Vec::new(),
    }
    .map(&automerge::ROOT, root)
}

/// What a slot holds now, owned, so the document can be written while
/// deciding.
enum Current {
    Missing,
    Map(ObjId),
    List(ObjId),
    Text(ObjId),
    Scalar(ScalarValue),
}

struct Reconciler<'a> {
    doc: &'a mut AutoCommit,
    shape: &'a Shape,
    // The JSON pointer of the value being reconciled, as segments, which
    // is what the shape's patterns match against.
    path: Vec<String>,
}

impl Reconciler<'_> {
    fn current(&self, obj: &ObjId, prop: Prop) -> Result<Current, AutomergeError> {
        Ok(match self.doc.get(obj, prop)? {
            None => Current::Missing,
            Some((Value::Object(ObjType::Map | ObjType::Table), id)) => Current::Map(id),
            Some((Value::Object(ObjType::List), id)) => Current::List(id),
            Some((Value::Object(ObjType::Text), id)) => Current::Text(id),
            Some((Value::Scalar(s), _)) => Current::Scalar(s.into_owned()),
        })
    }

    fn map(&mut self, obj: &ObjId, new: &Map<String, Json>) -> Result<(), AutomergeError> {
        let gone: Vec<String> = self
            .doc
            .keys(obj)
            .filter(|k| !new.contains_key(k))
            .collect();
        for k in gone {
            self.doc.delete(obj, k)?;
        }
        for (k, v) in new {
            self.path.push(k.clone());
            self.slot(obj, Prop::Map(k.clone()), v)?;
            self.path.pop();
        }
        Ok(())
    }

    /// Makes the value at an existing slot (a map key, or a list index
    /// that exists) equal to `new`, keeping the object there when its type
    /// is unchanged and replacing it when the type changed.
    fn slot(&mut self, obj: &ObjId, prop: Prop, new: &Json) -> Result<(), AutomergeError> {
        let current = self.current(obj, prop.clone())?;
        match new {
            Json::Object(m) => {
                let id = match current {
                    Current::Map(id) => id,
                    _ => self.doc.put_object(obj, prop, ObjType::Map)?,
                };
                self.map(&id, m)
            }
            Json::Array(a) => {
                let id = match current {
                    Current::List(id) => id,
                    _ => self.doc.put_object(obj, prop, ObjType::List)?,
                };
                self.list(&id, a)
            }
            Json::String(s) if self.shape.is_text(&self.path) => {
                match current {
                    // update_text diffs the old text against the new and
                    // splices only what changed, so a concurrent edit
                    // elsewhere in the sentence survives.
                    Current::Text(id) => {
                        if self.doc.text(&id)? != *s {
                            self.doc.update_text(&id, s)?;
                        }
                    }
                    _ => {
                        let id = self.doc.put_object(obj, prop, ObjType::Text)?;
                        self.doc.splice_text(&id, 0, 0, s)?;
                    }
                }
                Ok(())
            }
            _ => {
                let value = scalar(new);
                if let Current::Scalar(c) = &current {
                    if same(c, &value) {
                        return Ok(());
                    }
                }
                self.doc.put(obj, prop, value)
            }
        }
    }

    /// Inserts `new` as a fresh value at `index`.
    fn insert(&mut self, obj: &ObjId, index: usize, new: &Json) -> Result<(), AutomergeError> {
        match new {
            Json::Object(m) => {
                let id = self.doc.insert_object(obj, index, ObjType::Map)?;
                self.map(&id, m)
            }
            Json::Array(a) => {
                let id = self.doc.insert_object(obj, index, ObjType::List)?;
                self.list(&id, a)
            }
            Json::String(s) if self.shape.is_text(&self.path) => {
                let id = self.doc.insert_object(obj, index, ObjType::Text)?;
                self.doc.splice_text(&id, 0, 0, s)
            }
            _ => self.doc.insert(obj, index, scalar(new)),
        }
    }

    fn list(&mut self, obj: &ObjId, new: &[Json]) -> Result<(), AutomergeError> {
        match self.shape.list_key(&self.path) {
            Some(key) => {
                let key = key.to_string();
                self.keyed(obj, new, &key)
            }
            None => self.positional(obj, new),
        }
    }

    /// A list with no key: item i is item i.
    fn positional(&mut self, obj: &ObjId, new: &[Json]) -> Result<(), AutomergeError> {
        let old = self.doc.length(obj);
        for (i, v) in new.iter().enumerate() {
            self.path.push(i.to_string());
            if i < old {
                self.slot(obj, Prop::Seq(i), v)?;
            } else {
                self.insert(obj, i, v)?;
            }
            self.path.pop();
        }
        for i in (new.len()..old).rev() {
            self.doc.delete(obj, i)?;
        }
        Ok(())
    }

    /// A list of identified items: an item whose key is still there keeps
    /// its identity (and so any concurrent edit inside it), an item whose
    /// key is gone is deleted, and a new key is inserted where it stands.
    ///
    /// Automerge's lists have no move operation. An item that changed
    /// place is deleted and inserted again as a new object, which costs
    /// its identity: a concurrent edit inside the old copy is lost with
    /// it. So the items kept in place are the longest run that is already
    /// in the new order (a longest increasing subsequence), and only the
    /// others move. Items with no key are matched to each other in order.
    fn keyed(&mut self, obj: &ObjId, new: &[Json], key: &str) -> Result<(), AutomergeError> {
        let old_len = self.doc.length(obj);
        let mut old_keys = Vec::with_capacity(old_len);
        for i in 0..old_len {
            old_keys.push(self.item_key(obj, i, key)?);
        }

        let mut by_key: HashMap<String, usize> = HashMap::new();
        let mut keyless = Vec::new();
        for (j, v) in new.iter().enumerate() {
            match json_key(v, key) {
                Some(k) => {
                    by_key.entry(k).or_insert(j);
                }
                None => keyless.push(j),
            }
        }
        let mut keyless = keyless.into_iter();
        let mut taken = vec![false; new.len()];
        let mut matched: Vec<(usize, usize)> = Vec::new(); // (old index, new index)
        for (i, k) in old_keys.iter().enumerate() {
            let j = match k {
                Some(k) => by_key.get(k).copied().filter(|&j| !taken[j]),
                None => keyless.next(),
            };
            if let Some(j) = j {
                taken[j] = true;
                matched.push((i, j));
            }
        }

        let mut keep_old = vec![false; old_len];
        let mut keep_new = vec![false; new.len()];
        for idx in longest_increasing(&matched) {
            let (i, j) = matched[idx];
            keep_old[i] = true;
            keep_new[j] = true;
        }
        for i in (0..old_len).rev() {
            if !keep_old[i] {
                self.doc.delete(obj, i)?;
            }
        }
        // What is left is the kept items, in the new order; walk the new
        // list, reconciling each kept item and inserting the rest.
        for (pos, v) in new.iter().enumerate() {
            self.path.push(pos.to_string());
            if keep_new[pos] {
                self.slot(obj, Prop::Seq(pos), v)?;
            } else {
                self.insert(obj, pos, v)?;
            }
            self.path.pop();
        }
        Ok(())
    }

    fn item_key(
        &self,
        obj: &ObjId,
        index: usize,
        key: &str,
    ) -> Result<Option<String>, AutomergeError> {
        let item = match self.doc.get(obj, index)? {
            Some((Value::Object(ObjType::Map | ObjType::Table), id)) => id,
            _ => return Ok(None),
        };
        Ok(match self.doc.get(&item, key)? {
            Some((Value::Scalar(s), _)) => scalar_key(&s),
            Some((Value::Object(ObjType::Text), id)) => Some(format!("s{}", self.doc.text(&id)?)),
            _ => None,
        })
    }
}

/// The indexes into `pairs` (sorted by their first element) of a longest
/// run whose second elements strictly increase.
fn longest_increasing(pairs: &[(usize, usize)]) -> Vec<usize> {
    // tails[l] is the index of the smallest tail of a run of length l+1.
    let mut tails: Vec<usize> = Vec::new();
    let mut prev: Vec<Option<usize>> = vec![None; pairs.len()];
    for (idx, &(_, j)) in pairs.iter().enumerate() {
        let l = tails.partition_point(|&t| pairs[t].1 < j);
        if l > 0 {
            prev[idx] = Some(tails[l - 1]);
        }
        if l == tails.len() {
            tails.push(idx);
        } else {
            tails[l] = idx;
        }
    }
    let mut run = Vec::with_capacity(tails.len());
    let mut at = tails.last().copied();
    while let Some(idx) = at {
        run.push(idx);
        at = prev[idx];
    }
    run.reverse();
    run
}

fn json_key(v: &Json, key: &str) -> Option<String> {
    match v.as_object()?.get(key)? {
        Json::Object(_) | Json::Array(_) => None,
        k => scalar_key(&scalar(k)),
    }
}

/// A key's identity, typed, so the string "1" and the number 1 differ.
fn scalar_key(s: &ScalarValue) -> Option<String> {
    match s {
        ScalarValue::Str(s) => Some(format!("s{s}")),
        ScalarValue::Int(n) => Some(format!("n{n}")),
        ScalarValue::Uint(n) => Some(format!("n{n}")),
        ScalarValue::F64(f) => Some(format!("f{f}")),
        ScalarValue::Boolean(b) => Some(format!("b{b}")),
        _ => None,
    }
}

/// The scalar a JSON value is stored as: integral numbers in range as
/// integers, other numbers as floats.
pub fn scalar(v: &Json) -> ScalarValue {
    match v {
        Json::Null => ScalarValue::Null,
        Json::Bool(b) => ScalarValue::Boolean(*b),
        Json::String(s) => ScalarValue::Str(s.as_str().into()),
        Json::Number(n) => {
            if let Some(i) = n.as_i64() {
                return ScalarValue::Int(i);
            }
            let f = n.as_f64().unwrap_or(f64::NAN);
            // 2^63 is exactly representable; anything below it and at or
            // above -2^63 converts without loss.
            if f.fract() == 0.0 && (-9.223372036854776e18..9.223372036854776e18).contains(&f) {
                ScalarValue::Int(f as i64)
            } else {
                ScalarValue::F64(f)
            }
        }
        // Objects and arrays never reach here.
        Json::Object(_) | Json::Array(_) => ScalarValue::Null,
    }
}

/// Whether the stored scalar already reads as the new one, so writing it
/// would change nothing a reader sees.
fn same(current: &ScalarValue, new: &ScalarValue) -> bool {
    match (current, new) {
        (ScalarValue::Int(a), ScalarValue::Int(b)) => a == b,
        (ScalarValue::Uint(a), ScalarValue::Int(b)) => i128::from(*a) == i128::from(*b),
        (ScalarValue::Counter(_) | ScalarValue::Timestamp(_), ScalarValue::Int(b)) => {
            current.as_i64() == Some(*b)
        }
        (ScalarValue::F64(a), ScalarValue::F64(b)) => a == b,
        (ScalarValue::Str(a), ScalarValue::Str(b)) => a == b,
        (ScalarValue::Boolean(a), ScalarValue::Boolean(b)) => a == b,
        (ScalarValue::Null, ScalarValue::Null) => true,
        _ => false,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn longest_run_keeps_the_most_items() {
        let pairs = [(0, 2), (1, 0), (2, 1), (3, 3)];
        let run: Vec<usize> = longest_increasing(&pairs)
            .iter()
            .map(|&i| pairs[i].1)
            .collect();
        assert_eq!(run, vec![0, 1, 3]);
    }

    #[test]
    fn patterns_match_any_index() {
        let p = segments("/spec/phases/*/deliverables");
        let path: Vec<String> = ["spec", "phases", "3", "deliverables"]
            .iter()
            .map(|s| s.to_string())
            .collect();
        assert!(matches(&p, &path));
        assert!(!matches(&p, &path[..3]));
    }
}
