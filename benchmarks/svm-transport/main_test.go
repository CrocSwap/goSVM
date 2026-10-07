package main

import "testing"

func TestResponseComparisonPreservesUint64Precision(t *testing.T) {
	if equalJSON([]byte(`{"value":18446744073709551615}`), []byte(`{"value":18446744073709551614}`)) {
		t.Fatal("distinct uint64 values compare equal")
	}
	if !equalJSON([]byte(`{"a":1,"b":2}`), []byte(`{"b":2,"a":1}`)) {
		t.Fatal("object order affected comparison")
	}
}
func TestEnvelopeIdentityAndErrors(t *testing.T) {
	for _, data := range []string{`{"schema":2,"id":1,"result":[]}`, `{"schema":1,"id":2,"result":[]}`, `{"schema":1,"id":1,"error":{"message":"rejected"}}`} {
		if _, e := unpack([]byte(data), 1); e == nil {
			t.Fatal("invalid envelope accepted")
		}
	}
}
