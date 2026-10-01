//! The Automerge module the engine runs (docs/adr/0007).
//!
//! The engine's adapter (internal/crdt/automerge) loads this crate,
//! compiled to wasm32-wasip1, into wazero and calls the functions below.
//! The interface is deliberately narrow and batch-oriented: one call per
//! operation, with whole documents, saves and sync messages crossing the
//! boundary as bytes (JSON where the data is structured), never one call
//! per field. Crossing the boundary costs a copy, so fewer, larger calls
//! are cheaper.
//!
//! Conventions every export follows:
//!
//! - Byte arguments are a pointer and a length into this module's memory,
//!   written by the caller into a buffer from `cg_alloc`. The caller keeps
//!   ownership and frees it with `cg_free`.
//! - A function returns an `i32`: zero or a positive value (a handle, or a
//!   flag) on success, a negative error code on failure.
//! - A function that produces bytes leaves them in the output buffer,
//!   which `cg_out` returns as `ptr << 32 | len`. On failure the output
//!   buffer holds the error message, UTF-8. The buffer is valid until the
//!   next call, so the caller copies it at once.
//! - Documents and sync states are handles, positive `i32`s, valid until
//!   freed.
//!
//! Randomness for actor ids comes from WASI `random_get`, which the host
//! provides.

mod materialise;
mod reconcile;

use std::alloc::{alloc, dealloc, Layout};
use std::cell::RefCell;
use std::collections::HashMap;

use automerge::sync::{self, SyncDoc};
use automerge::transaction::{CommitOptions, Transactable};
use automerge::{AutoCommit, AutomergeError, ChangeHash};

/// A handle that names nothing: never issued, or already freed.
const ERR_HANDLE: i32 = -1;
/// Input the module could not read: malformed JSON, a bad shape.
const ERR_INPUT: i32 = -2;
/// Automerge refused the operation: corrupt bytes, a bad sync message.
const ERR_AUTOMERGE: i32 = -3;

struct Error {
    code: i32,
    message: String,
}

impl Error {
    fn input(message: impl Into<String>) -> Error {
        Error {
            code: ERR_INPUT,
            message: message.into(),
        }
    }
}

impl From<AutomergeError> for Error {
    fn from(e: AutomergeError) -> Error {
        Error {
            code: ERR_AUTOMERGE,
            message: e.to_string(),
        }
    }
}

/// One document, and the heads of its last save, so that an incremental
/// save holds exactly the changes since the last save or load.
/// `AutoCommit` keeps a cursor of its own, but `load` leaves it empty, and
/// the port promises "since the last Save, SaveIncremental or Load".
struct Document {
    doc: AutoCommit,
    saved: Vec<ChangeHash>,
}

#[derive(Default)]
struct Handles {
    docs: HashMap<i32, Document>,
    syncs: HashMap<i32, sync::State>,
    last: i32,
}

impl Handles {
    fn issue(&mut self) -> i32 {
        loop {
            self.last = if self.last == i32::MAX {
                1
            } else {
                self.last + 1
            };
            if !self.docs.contains_key(&self.last) && !self.syncs.contains_key(&self.last) {
                return self.last;
            }
        }
    }

    fn doc(&mut self, h: i32) -> Result<&mut Document, Error> {
        self.docs
            .get_mut(&h)
            .ok_or_else(|| no_handle("document", h))
    }
}

fn no_handle(what: &str, h: i32) -> Error {
    Error {
        code: ERR_HANDLE,
        message: format!("no {what} with handle {h}"),
    }
}

// The module is single-threaded: wasm32-wasip1 has one thread, and the
// host serialises calls into an instance. Thread-locals are how Rust
// spells a global there without unsafe.
thread_local! {
    static HANDLES: RefCell<Handles> = RefCell::new(Handles::default());
    static OUT: RefCell<Vec<u8>> = const { RefCell::new(Vec::new()) };
}

fn set_out(bytes: Vec<u8>) {
    OUT.with(|o| *o.borrow_mut() = bytes);
}

fn with_handles<F: FnOnce(&mut Handles) -> Result<i32, Error>>(f: F) -> i32 {
    match HANDLES.with(|h| f(&mut h.borrow_mut())) {
        Ok(v) => v,
        Err(e) => {
            set_out(e.message.into_bytes());
            e.code
        }
    }
}

/// Reads an argument the host wrote into this module's memory.
///
/// # Safety
///
/// `ptr` and `len` must describe memory the host wrote, from `cg_alloc`.
unsafe fn arg<'a>(ptr: *const u8, len: u32) -> &'a [u8] {
    if len == 0 || ptr.is_null() {
        &[]
    } else {
        std::slice::from_raw_parts(ptr, len as usize)
    }
}

fn layout(len: u32) -> Layout {
    // Zero-sized allocations are undefined; one byte is the floor.
    Layout::from_size_align(len.max(1) as usize, 8).expect("layout")
}

/// Allocates `len` bytes for the host to write an argument into.
#[no_mangle]
pub extern "C" fn cg_alloc(len: u32) -> *mut u8 {
    unsafe { alloc(layout(len)) }
}

/// Frees a buffer from `cg_alloc`.
///
/// # Safety
///
/// `ptr` must come from `cg_alloc(len)` and not be freed already.
#[no_mangle]
pub unsafe extern "C" fn cg_free(ptr: *mut u8, len: u32) {
    if !ptr.is_null() {
        dealloc(ptr, layout(len));
    }
}

/// The output buffer of the last call, as `ptr << 32 | len`.
#[no_mangle]
pub extern "C" fn cg_out() -> u64 {
    OUT.with(|o| {
        let o = o.borrow();
        ((o.as_ptr() as u64) << 32) | o.len() as u64
    })
}

/// A new, empty document with a random actor.
#[no_mangle]
pub extern "C" fn cg_doc_new() -> i32 {
    with_handles(|h| {
        let id = h.issue();
        h.docs.insert(
            id,
            Document {
                doc: AutoCommit::new(),
                saved: Vec::new(),
            },
        );
        Ok(id)
    })
}

/// A document from saved bytes (a snapshot and any chunks after it), with
/// a random actor for the changes it makes.
///
/// # Safety
///
/// See [`arg`].
#[no_mangle]
pub unsafe extern "C" fn cg_doc_load(ptr: *const u8, len: u32) -> i32 {
    let data = arg(ptr, len);
    with_handles(|h| {
        let mut doc = AutoCommit::load(data)?;
        let saved = doc.get_heads();
        let id = h.issue();
        h.docs.insert(id, Document { doc, saved });
        Ok(id)
    })
}

#[no_mangle]
pub extern "C" fn cg_doc_free(doc: i32) -> i32 {
    with_handles(|h| {
        h.docs
            .remove(&doc)
            .ok_or_else(|| no_handle("document", doc))?;
        Ok(0)
    })
}

/// The whole document, compacted, into the output buffer.
#[no_mangle]
pub extern "C" fn cg_doc_save(doc: i32) -> i32 {
    with_handles(|h| {
        let d = h.doc(doc)?;
        let bytes = d.doc.save();
        d.saved = d.doc.get_heads();
        set_out(bytes);
        Ok(0)
    })
}

/// The changes since the last save or load into the output buffer; empty
/// when there are none.
#[no_mangle]
pub extern "C" fn cg_doc_save_incremental(doc: i32) -> i32 {
    with_handles(|h| {
        let d = h.doc(doc)?;
        let bytes = d.doc.save_after(&d.saved);
        d.saved = d.doc.get_heads();
        set_out(bytes);
        Ok(0)
    })
}

/// Applies saved bytes (a snapshot or chunks). Changes the document
/// already holds are skipped, so applying the same bytes twice is safe.
///
/// # Safety
///
/// See [`arg`].
#[no_mangle]
pub unsafe extern "C" fn cg_doc_load_incremental(doc: i32, ptr: *const u8, len: u32) -> i32 {
    let data = arg(ptr, len);
    with_handles(|h| {
        h.doc(doc)?.doc.load_incremental(data)?;
        Ok(0)
    })
}

/// An independent copy with a new random actor; returns its handle.
#[no_mangle]
pub extern "C" fn cg_doc_fork(doc: i32) -> i32 {
    with_handles(|h| {
        let forked = h.doc(doc)?.doc.fork();
        let id = h.issue();
        h.docs.insert(
            id,
            Document {
                doc: forked,
                saved: Vec::new(),
            },
        );
        Ok(id)
    })
}

/// The materialised document, as JSON, into the output buffer.
#[no_mangle]
pub extern "C" fn cg_doc_json(doc: i32) -> i32 {
    with_handles(|h| {
        let d = h.doc(doc)?;
        set_out(materialise::json(&d.doc)?);
        Ok(0)
    })
}

/// Makes the document equal to a JSON object with the fewest operations,
/// following a shape (`{"listKeys": {pointer: field}, "texts": [pointer]}`),
/// and records them as one change with a message and a time in Unix
/// milliseconds (zero records none). Returns 1 when anything changed, 0
/// when the document already matched.
///
/// # Safety
///
/// See [`arg`].
#[no_mangle]
#[allow(clippy::too_many_arguments)]
pub unsafe extern "C" fn cg_doc_reconcile(
    doc: i32,
    json_ptr: *const u8,
    json_len: u32,
    shape_ptr: *const u8,
    shape_len: u32,
    msg_ptr: *const u8,
    msg_len: u32,
    time_ms: i64,
) -> i32 {
    let json = arg(json_ptr, json_len);
    let shape = arg(shape_ptr, shape_len);
    let message = arg(msg_ptr, msg_len);
    with_handles(|h| {
        let value: serde_json::Value =
            serde_json::from_slice(json).map_err(|e| Error::input(format!("document: {e}")))?;
        let root = value
            .as_object()
            .ok_or_else(|| Error::input("document: not an object"))?;
        let shape = reconcile::Shape::parse(shape).map_err(Error::input)?;
        let message =
            std::str::from_utf8(message).map_err(|e| Error::input(format!("message: {e}")))?;
        let d = h.doc(doc)?;
        if let Err(e) = reconcile::reconcile(&mut d.doc, root, &shape) {
            d.doc.rollback();
            return Err(e.into());
        }
        if d.doc.pending_ops() == 0 {
            d.doc.rollback();
            return Ok(0);
        }
        let mut options = CommitOptions::default();
        if !message.is_empty() {
            options.set_message(message);
        }
        if time_ms != 0 {
            // Automerge records a change's time in seconds, as its other
            // implementations read it.
            options.set_time(time_ms.div_euclid(1000));
        }
        d.doc.commit_with(options);
        Ok(1)
    })
}

/// The heads, hex-encoded and sorted, as a JSON array into the output
/// buffer.
#[no_mangle]
pub extern "C" fn cg_doc_heads(doc: i32) -> i32 {
    with_handles(|h| {
        let mut heads: Vec<String> = h
            .doc(doc)?
            .doc
            .get_heads()
            .iter()
            .map(|c| c.to_string())
            .collect();
        heads.sort();
        set_out(serde_json::to_vec(&heads).expect("strings encode"));
        Ok(0)
    })
}

/// Every field holding concurrent values, as a JSON array of
/// `{"path": pointer, "values": [winner, others...]}` into the output
/// buffer.
#[no_mangle]
pub extern "C" fn cg_doc_conflicts(doc: i32) -> i32 {
    with_handles(|h| {
        let d = h.doc(doc)?;
        set_out(materialise::conflicts(&d.doc)?);
        Ok(0)
    })
}

/// A new sync state, for one peer of one document.
#[no_mangle]
pub extern "C" fn cg_sync_new() -> i32 {
    with_handles(|h| {
        let id = h.issue();
        h.syncs.insert(id, sync::State::new());
        Ok(id)
    })
}

#[no_mangle]
pub extern "C" fn cg_sync_free(state: i32) -> i32 {
    with_handles(|h| {
        h.syncs
            .remove(&state)
            .ok_or_else(|| no_handle("sync state", state))?;
        Ok(0)
    })
}

/// The next sync message for the peer into the output buffer, returning
/// 1; or 0, with nothing to send, when the peer is up to date or a
/// message is already in flight.
#[no_mangle]
pub extern "C" fn cg_sync_generate(doc: i32, state: i32) -> i32 {
    with_handles(|h| {
        let Handles { docs, syncs, .. } = h;
        let d = docs
            .get_mut(&doc)
            .ok_or_else(|| no_handle("document", doc))?;
        let s = syncs
            .get_mut(&state)
            .ok_or_else(|| no_handle("sync state", state))?;
        match d.doc.sync().generate_sync_message(s) {
            Some(m) => {
                set_out(m.encode());
                Ok(1)
            }
            None => Ok(0),
        }
    })
}

/// Applies a sync message from the peer.
///
/// # Safety
///
/// See [`arg`].
#[no_mangle]
pub unsafe extern "C" fn cg_sync_receive(doc: i32, state: i32, ptr: *const u8, len: u32) -> i32 {
    let data = arg(ptr, len);
    with_handles(|h| {
        let Handles { docs, syncs, .. } = h;
        let d = docs
            .get_mut(&doc)
            .ok_or_else(|| no_handle("document", doc))?;
        let s = syncs
            .get_mut(&state)
            .ok_or_else(|| no_handle("sync state", state))?;
        let m =
            sync::Message::decode(data).map_err(|e| Error::input(format!("sync message: {e}")))?;
        d.doc.sync().receive_sync_message(s, m)?;
        Ok(0)
    })
}
