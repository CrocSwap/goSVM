package main

/*
#cgo linux LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdlib.h>

typedef void* (*new_fn)(void);
typedef void (*free_fn)(void*);
typedef char* (*request_fn)(void*, const char*);
typedef void (*response_free_fn)(char*);
static void* lib_handle;
static new_fn session_new;
static free_fn session_free;
static request_fn session_request;
static response_free_fn response_free;
static int open_library(const char* path) {
    lib_handle=dlopen(path, RTLD_NOW|RTLD_LOCAL);
    if (!lib_handle) return 0;
    session_new=(new_fn)dlsym(lib_handle,"gosvm_session_new");
    session_free=(free_fn)dlsym(lib_handle,"gosvm_session_free");
    session_request=(request_fn)dlsym(lib_handle,"gosvm_session_request");
    response_free=(response_free_fn)dlsym(lib_handle,"gosvm_response_free");
    if (!session_new || !session_free || !session_request || !response_free) return 0;
    return 1;
}
static void* new_session(void) {return session_new();}
static void close_session(void* p) {session_free(p);}
static char* call_session(void* p,const char* s) {return session_request(p,s);}
static void close_response(char* p) {response_free(p);}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func openLibrary(path string) error {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	if C.open_library(p) == 0 {
		return fmt.Errorf("dlopen/dlsym failed for %s", path)
	}
	return nil
}

type embeddedClient struct{ handle unsafe.Pointer }

func newEmbedded() *embeddedClient { return &embeddedClient{C.new_session()} }
func (c *embeddedClient) request(input []byte) ([]byte, error) {
	p := C.CString(string(input))
	defer C.free(unsafe.Pointer(p))
	r := C.call_session(c.handle, p)
	if r == nil {
		return nil, fmt.Errorf("embedded session returned null")
	}
	defer C.close_response(r)
	return []byte(C.GoString(r)), nil
}
func (c *embeddedClient) close() error {
	if c.handle != nil {
		C.close_session(c.handle)
		c.handle = nil
	}
	return nil
}
