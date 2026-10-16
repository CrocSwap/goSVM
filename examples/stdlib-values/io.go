package stdlibvalues

import (
	"errors"
	"io"
)

// IOOps is the number of operations of the io group.
const IOOps = 6

// chunkReader returns its data at most chunk bytes per Read, then fail (or
// io.EOF when fail is nil). With both set, the last data and the error come
// together.
type chunkReader struct {
	data  []byte
	chunk int
	fail  error
	both  bool
	reads int
}

func (r *chunkReader) Read(p []byte) (int, error) {
	r.reads++
	if len(r.data) == 0 {
		if r.fail != nil {
			return 0, r.fail
		}
		return 0, io.EOF
	}
	k := len(p)
	if r.chunk < k {
		k = r.chunk
	}
	n := copy(p[:k], r.data)
	r.data = r.data[n:]
	if r.both && len(r.data) == 0 {
		if r.fail != nil {
			return n, r.fail
		}
		return n, io.EOF
	}
	return n, nil
}

// sink is an io.Writer that keeps at most limit bytes per call.
type sink struct {
	got   []byte
	limit int
	calls int
}

func (s *sink) Write(p []byte) (int, error) {
	s.calls++
	n := len(p)
	if s.limit < n {
		n = s.limit
	}
	s.got = append(s.got, p[:n]...)
	if n < len(p) {
		return n, io.ErrShortWrite
	}
	return n, nil
}

// stringSink is also an io.StringWriter.
type stringSink struct{ s sink }

func (s *stringSink) Write(p []byte) (int, error) { return s.s.Write(p) }

func (s *stringSink) WriteString(str string) (int, error) {
	s.s.calls += 100
	s.s.got = append(s.s.got, str...)
	return len(str), nil
}

func (w *W) ioErr(err error) {
	w.err(err)
	for _, e := range []error{io.EOF, io.ErrUnexpectedEOF, io.ErrShortBuffer, io.ErrShortWrite, io.ErrNoProgress} {
		w.flag(err == e)
		w.flag(errors.Is(err, e))
	}
}

func ioGroup(w *W, v Input) {
	failure := []error{nil, io.ErrNoProgress, errors.New("custom"), &wrapErr{"eof", io.EOF}}[v.h3%4]
	r := &chunkReader{data: []byte(v.a), chunk: v.h0%5 + 1, fail: failure, both: v.x&1 != 0}
	switch v.op % IOOps {
	case 0:
		buf := make([]byte, v.h1%60)
		n, err := io.ReadFull(r, buf)
		w.int(n)
		w.ioErr(err)
		w.bs(buf)
		w.int(r.reads)
	case 1:
		buf := make([]byte, v.h1%60)
		n, err := io.ReadAtLeast(r, buf, v.h2%70)
		w.int(n)
		w.ioErr(err)
		w.bs(buf)
		w.int(r.reads)
	case 2:
		// A nil buffer and a zero minimum.
		n, err := io.ReadAtLeast(r, nil, v.h2%2)
		w.int(n)
		w.ioErr(err)
		n, err = io.ReadFull(r, nil)
		w.int(n)
		w.ioErr(err)
	case 3:
		var dst io.Writer
		var s *sink
		if v.x&2 != 0 {
			ss := &stringSink{sink{limit: v.h1 % 50}}
			dst, s = ss, &ss.s
		} else {
			s = &sink{limit: v.h1 % 50}
			dst = s
		}
		n, err := io.WriteString(dst, v.b)
		w.int(n)
		w.ioErr(err)
		w.bs(s.got)
		w.int(s.calls)
	case 4:
		for _, e := range []error{io.EOF, io.ErrUnexpectedEOF, io.ErrShortBuffer, io.ErrShortWrite, io.ErrNoProgress} {
			w.str(e.Error())
		}
		w.int(io.SeekStart)
		w.int(io.SeekCurrent)
		w.int(io.SeekEnd)
		w.ioErr(&wrapErr{"x", io.ErrUnexpectedEOF})
	case 5:
		// Interface satisfaction and assertions between the io interfaces.
		var x any = r
		if v.x&4 != 0 {
			x = &stringSink{}
		}
		_, isReader := x.(io.Reader)
		_, isWriter := x.(io.Writer)
		_, isStringWriter := x.(io.StringWriter)
		_, isReadWriter := x.(io.ReadWriter)
		_, isByteReader := x.(io.ByteReader)
		w.flag(isReader)
		w.flag(isWriter)
		w.flag(isStringWriter)
		w.flag(isReadWriter)
		w.flag(isByteReader)
	}
}
