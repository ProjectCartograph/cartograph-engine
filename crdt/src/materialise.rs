//! Reading a document out as JSON, and finding its conflicts.

use automerge::{AutoCommit, AutomergeError, ObjId, ObjType, Prop, ReadDoc, ScalarValue, Value};
use serde_json::{json, Map, Number, Value as Json};

/// The document as JSON: maps as objects, lists as arrays, texts and
/// strings as strings, counters and timestamps (milliseconds) as numbers,
/// bytes as standard base64.
pub fn json(doc: &AutoCommit) -> Result<Vec<u8>, AutomergeError> {
    let v = object(doc, &automerge::ROOT, ObjType::Map)?;
    Ok(serde_json::to_vec(&v).expect("a JSON value encodes"))
}

fn object(doc: &AutoCommit, id: &ObjId, ty: ObjType) -> Result<Json, AutomergeError> {
    Ok(match ty {
        ObjType::Map | ObjType::Table => {
            let mut m = Map::new();
            for k in doc.keys(id) {
                if let Some((v, vid)) = doc.get(id, k.as_str())? {
                    m.insert(k, value(doc, v, &vid)?);
                }
            }
            Json::Object(m)
        }
        ObjType::List => {
            let n = doc.length(id);
            let mut a = Vec::with_capacity(n);
            for i in 0..n {
                if let Some((v, vid)) = doc.get(id, i)? {
                    a.push(value(doc, v, &vid)?);
                }
            }
            Json::Array(a)
        }
        ObjType::Text => Json::String(doc.text(id)?),
    })
}

fn value(doc: &AutoCommit, v: Value<'_>, id: &ObjId) -> Result<Json, AutomergeError> {
    match v {
        Value::Object(ty) => object(doc, id, ty),
        Value::Scalar(s) => Ok(scalar(&s)),
    }
}

fn scalar(s: &ScalarValue) -> Json {
    match s {
        ScalarValue::Str(s) => Json::String(s.to_string()),
        ScalarValue::Int(n) => json!(n),
        ScalarValue::Uint(n) => json!(n),
        ScalarValue::F64(f) => Number::from_f64(*f).map(Json::Number).unwrap_or(Json::Null),
        ScalarValue::Counter(_) | ScalarValue::Timestamp(_) => json!(s.as_i64().unwrap_or(0)),
        ScalarValue::Boolean(b) => Json::Bool(*b),
        ScalarValue::Bytes(b) => Json::String(base64(b)),
        ScalarValue::Null | ScalarValue::Unknown { .. } => Json::Null,
    }
}

/// Every map key and list index holding more than one value, in document
/// order (map keys sorted, list items by index), as JSON:
/// `[{"path": pointer, "values": [winner, ...others]}]`. The winner is the
/// value the materialised document shows; the others follow in Automerge's
/// order, which every replica shares. Only the winning branch of an
/// object is walked, since that is the one a reader sees.
pub fn conflicts(doc: &AutoCommit) -> Result<Vec<u8>, AutomergeError> {
    let mut out = Vec::new();
    walk(
        doc,
        &automerge::ROOT,
        ObjType::Map,
        &mut String::new(),
        &mut out,
    )?;
    Ok(serde_json::to_vec(&out).expect("a JSON value encodes"))
}

fn walk(
    doc: &AutoCommit,
    id: &ObjId,
    ty: ObjType,
    path: &mut String,
    out: &mut Vec<Json>,
) -> Result<(), AutomergeError> {
    match ty {
        ObjType::Map | ObjType::Table => {
            for k in doc.keys(id) {
                let n = path.len();
                path.push('/');
                path.push_str(&k.replace('~', "~0").replace('/', "~1"));
                slot(doc, id, Prop::Map(k), path, out)?;
                path.truncate(n);
            }
        }
        ObjType::List => {
            for i in 0..doc.length(id) {
                let n = path.len();
                path.push('/');
                path.push_str(&i.to_string());
                slot(doc, id, Prop::Seq(i), path, out)?;
                path.truncate(n);
            }
        }
        // A text merges character by character; it has no conflicts a
        // reader would choose between.
        ObjType::Text => {}
    }
    Ok(())
}

fn slot(
    doc: &AutoCommit,
    id: &ObjId,
    prop: Prop,
    path: &mut String,
    out: &mut Vec<Json>,
) -> Result<(), AutomergeError> {
    let Some((winner, wid)) = doc.get(id, prop.clone())? else {
        return Ok(());
    };
    let all = doc.get_all(id, prop)?;
    if all.len() > 1 {
        let mut values = vec![value(doc, winner.clone(), &wid)?];
        for (v, vid) in all {
            if vid != wid {
                values.push(value(doc, v, &vid)?);
            }
        }
        out.push(json!({ "path": path.as_str(), "values": values }));
    }
    if let Value::Object(ty) = winner {
        walk(doc, &wid, ty, path, out)?;
    }
    Ok(())
}

fn base64(bytes: &[u8]) -> String {
    const ALPHABET: &[u8; 64] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    let mut s = String::with_capacity(bytes.len().div_ceil(3) * 4);
    for chunk in bytes.chunks(3) {
        let b = [
            chunk[0],
            *chunk.get(1).unwrap_or(&0),
            *chunk.get(2).unwrap_or(&0),
        ];
        let n = (u32::from(b[0]) << 16) | (u32::from(b[1]) << 8) | u32::from(b[2]);
        for (i, shift) in [18, 12, 6, 0].into_iter().enumerate() {
            if i <= chunk.len() {
                s.push(ALPHABET[((n >> shift) & 63) as usize] as char);
            } else {
                s.push('=');
            }
        }
    }
    s
}

#[cfg(test)]
mod tests {
    use super::base64;

    #[test]
    fn base64_matches_the_standard_encoding() {
        assert_eq!(base64(b""), "");
        assert_eq!(base64(b"f"), "Zg==");
        assert_eq!(base64(b"fo"), "Zm8=");
        assert_eq!(base64(b"foo"), "Zm9v");
        assert_eq!(base64(b"foob"), "Zm9vYg==");
    }
}
