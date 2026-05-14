// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build cgo && !nwrfc_nosdk

// tRFC and qRFC client bindings.
//
// Per sapnwrfc.h §2120 the SDK distinguishes tRFC from qRFC by
// the queueName argument to a single creator: NULL means
// transactional, non-NULL means queued (qRFC). This file
// implements the [backend.TransactionalBackend] interface over
// the SDK's RfcCreateTransaction / RfcInvokeInTransaction /
// RfcSubmitTransaction / RfcConfirmTransaction /
// RfcDestroyTransaction calls.
//
// Earlier doc drafts referenced a `RfcSetQueueName` symbol that
// does NOT exist in the SDK. The qRFC variant lives entirely in
// RfcCreateTransaction.

package sdkbackend

/*
#include "helpers.h"
*/
import "C"

import (
	"context"
	"fmt"
	"sync"
	"unsafe"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// txnHandle is the per-transaction state owned by the backend.
type txnHandle struct {
	id     backend.TxHandle
	connID backend.ConnHandle
	// sdkPtr is the cgo-wrapped RFC_TRANSACTION_HANDLE,
	// stored as a uintptr so backend_sdk.go does not need to
	// import "C". Round-trip through unsafe.Pointer is
	// well-defined for opaque cgo handles.
	sdkPtr uintptr
	mu     sync.Mutex
}

// txnRegistry maps opaque IDs to *txnHandle. Mirrors the
// connRegistry pattern so we never leak unsafe.Pointer across
// the public boundary.
type txnRegistry struct {
	mu   sync.Mutex
	next uint64
	m    map[backend.TxHandle]*txnHandle
}

func (r *txnRegistry) put(t *txnHandle) backend.TxHandle {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	id := backend.TxHandle(r.next)
	t.id = id
	r.m[id] = t
	return id
}

func (r *txnRegistry) get(id backend.TxHandle) (*txnHandle, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.m[id]
	if !ok {
		return nil, fmt.Errorf("nwrfc/sdkbackend: unknown transaction handle %d", id)
	}
	return t, nil
}

func (r *txnRegistry) remove(id backend.TxHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, id)
}

// txns is the package singleton transaction registry. Wired
// during init() in backend_sdk.go so the registry is ready
// before any backend method is called.
var txns = txnRegistry{m: make(map[backend.TxHandle]*txnHandle)}

func sdkTxPtr(t *txnHandle) C.RFC_TRANSACTION_HANDLE {
	return (C.RFC_TRANSACTION_HANDLE)(unsafe.Pointer(t.sdkPtr))
}

// CreateTransaction implements
// [backend.TransactionalBackend.CreateTransaction].
//
// SDK function: RfcCreateTransaction (sapnwrfc.h §2131).
// queueName="" → tRFC (passes NULL); non-empty → qRFC.
func (b *sdkBackend) CreateTransaction(h backend.ConnHandle, tid string, queueName string) (backend.TxHandle, error) {
	c, err := b.conns.get(h)
	if err != nil {
		return 0, err
	}
	if len(tid) != 24 {
		return 0, fmt.Errorf("nwrfc/sdkbackend: TID must be exactly 24 chars, got %d", len(tid))
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	tidUC, err := stringToSAPUC(tid)
	if err != nil {
		return 0, err
	}
	defer C.goFreeU(tidUC)

	var queueUC *C.SAP_UC
	if queueName != "" {
		queueUC, err = stringToSAPUC(queueName)
		if err != nil {
			return 0, err
		}
		defer C.goFreeU(queueUC)
	}

	var info C.RFC_ERROR_INFO
	// RfcCreateTransaction signature:
	//   RFC_TRANSACTION_HANDLE RfcCreateTransaction(
	//       RFC_CONNECTION_HANDLE rfcHandle,
	//       RFC_TID tid,           // SAP_UC[25]
	//       SAP_UC const *queueName, // NULL for tRFC
	//       RFC_ERROR_INFO* errorInfo)
	//
	// RFC_TID is `SAP_UC[RFC_TID_LN+1]` = `SAP_UC[25]`. Cgo
	// represents the array as (*C.SAP_UC) when passed by name;
	// since stringToSAPUC produced a zero-terminated buffer of
	// at least len(tid)+1 = 25 SAP_UC code units, the cast is
	// safe. The SDK reads up to 24 chars and the trailing null.
	tHandle := C.RfcCreateTransaction(
		sdkConnPtr(c),
		(*C.SAP_UC)(unsafe.Pointer(tidUC)),
		queueUC, // C handles NULL when queueName=="" because we left it nil
		&info,
	)
	if tHandle == nil {
		op := "RfcCreateTransaction(tRFC)"
		if queueName != "" {
			op = "RfcCreateTransaction(qRFC, queue=" + queueName + ")"
		}
		return 0, errFromInfo(&info, op)
	}

	t := &txnHandle{
		connID: h,
		sdkPtr: uintptr(unsafe.Pointer(tHandle)),
	}
	return txns.put(t), nil
}

// InvokeInTransaction implements
// [backend.TransactionalBackend.InvokeInTransaction].
//
// Multiple calls on the same TxHandle become one LUW. Note that
// tRFC/qRFC calls do NOT carry return values back: per
// sapnwrfc.h §2139, "EXPORTING parameters of this function
// handle will not be filled, nor will the changes to the
// CHANGING/TABLES parameters be returned". We therefore ignore
// `opts.NotRequested` for return-direction filtering and only
// apply it to the IMPORT side (consistent behaviour with
// node-rfc).
//
// SDK function: RfcInvokeInTransaction (sapnwrfc.h §2144).
func (b *sdkBackend) InvokeInTransaction(ctx context.Context, h backend.ConnHandle, tx backend.TxHandle, fn string, in backend.CallParams, opts backend.InvokeOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c, err := b.conns.get(h)
	if err != nil {
		return err
	}
	t, err := txns.get(tx)
	if err != nil {
		return err
	}
	if t.connID != h {
		return fmt.Errorf("nwrfc/sdkbackend: transaction %d belongs to a different connection", tx)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	t.mu.Lock()
	defer t.mu.Unlock()

	nameUC, err := stringToSAPUC(fn)
	if err != nil {
		return err
	}
	defer C.goFreeU(nameUC)

	var info C.RFC_ERROR_INFO
	desc := C.RfcGetFunctionDesc(sdkConnPtr(c), nameUC, &info)
	if desc == nil {
		return errFromInfo(&info, "RfcGetFunctionDesc("+fn+")")
	}

	fh := C.RfcCreateFunction(desc, &info)
	if fh == nil {
		return errFromInfo(&info, "RfcCreateFunction("+fn+")")
	}
	defer C.RfcDestroyFunction(fh, &info)

	if err := fillFunctionParameters(fh, desc, in); err != nil {
		return err
	}

	// notRequested handling on IMPORT side, same as Invoke.
	for _, name := range opts.NotRequested {
		uc, err := stringToSAPUC(name)
		if err != nil {
			return err
		}
		rc := C.goSetParamActive(fh, uc, 0, &info)
		C.goFreeU(uc)
		if rc != C.RFC_OK {
			return errFromInfo(&info, "RfcSetParameterActive("+name+")")
		}
	}

	// ctx cancel watcher — RfcCancel applies on the connection
	// handle (not the transaction handle); shared via cancel.go
	// to keep one source of truth for the goroutine pattern.
	cleanup := withCancelWatcher(ctx, c)
	rc := C.RfcInvokeInTransaction(sdkTxPtr(t), fh, &info)
	cleanup()

	if rc != C.RFC_OK {
		if err := ctxErrorIfFired(ctx, "RfcInvokeInTransaction("+fn+")"); err != nil {
			return err
		}
		return errFromInfo(&info, "RfcInvokeInTransaction("+fn+")")
	}
	return nil
}

// SubmitTransaction implements
// [backend.TransactionalBackend.SubmitTransaction].
//
// SDK function: RfcSubmitTransaction (sapnwrfc.h §2159).
func (b *sdkBackend) SubmitTransaction(tx backend.TxHandle) error {
	t, err := txns.get(tx)
	if err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	var info C.RFC_ERROR_INFO
	rc := C.RfcSubmitTransaction(sdkTxPtr(t), &info)
	if rc != C.RFC_OK {
		return errFromInfo(&info, "RfcSubmitTransaction")
	}
	return nil
}

// ConfirmTransaction implements
// [backend.TransactionalBackend.ConfirmTransaction].
//
// SDK function: RfcConfirmTransaction (sapnwrfc.h §2176).
func (b *sdkBackend) ConfirmTransaction(tx backend.TxHandle) error {
	t, err := txns.get(tx)
	if err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	var info C.RFC_ERROR_INFO
	rc := C.RfcConfirmTransaction(sdkTxPtr(t), &info)
	if rc != C.RFC_OK {
		return errFromInfo(&info, "RfcConfirmTransaction")
	}
	return nil
}

// DestroyTransaction implements
// [backend.TransactionalBackend.DestroyTransaction]. Removes
// the entry from the registry on success.
//
// SDK function: RfcDestroyTransaction (sapnwrfc.h §2208).
func (b *sdkBackend) DestroyTransaction(tx backend.TxHandle) error {
	t, err := txns.get(tx)
	if err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	var info C.RFC_ERROR_INFO
	rc := C.RfcDestroyTransaction(sdkTxPtr(t), &info)
	txns.remove(tx)
	if rc != C.RFC_OK {
		return errFromInfo(&info, "RfcDestroyTransaction")
	}
	return nil
}
