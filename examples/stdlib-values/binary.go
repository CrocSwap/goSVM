package stdlibvalues

import "encoding/binary"

// BinaryOps is the number of operations of the binary group.
const BinaryOps = 6

// orders returns the byte order selected by k, through the interfaces.
func orders(k int) (binary.ByteOrder, binary.AppendByteOrder) {
	switch k % 3 {
	case 0:
		return binary.LittleEndian, binary.LittleEndian
	case 1:
		return binary.BigEndian, binary.BigEndian
	}
	return binary.NativeEndian, binary.NativeEndian
}

func binaryGroup(w *W, v Input) {
	in := v.raw
	switch v.op % BinaryOps {
	case 0: // reads, directly and through ByteOrder
		w.u64(uint64(binary.LittleEndian.Uint16(in[8:])))
		w.u64(uint64(binary.LittleEndian.Uint32(in[8:])))
		w.u64(binary.LittleEndian.Uint64(in[8:]))
		w.u64(uint64(binary.BigEndian.Uint16(in[9:])))
		w.u64(uint64(binary.BigEndian.Uint32(in[9:])))
		w.u64(binary.BigEndian.Uint64(in[9:]))
		w.u64(uint64(binary.NativeEndian.Uint16(in[10:])))
		w.u64(uint64(binary.NativeEndian.Uint32(in[10:])))
		w.u64(binary.NativeEndian.Uint64(in[10:]))
		o, _ := orders(v.h0)
		w.u64(uint64(o.Uint16(in[11:])))
		w.u64(uint64(o.Uint32(in[11:])))
		w.u64(o.Uint64(in[11:]))
		w.str(o.String())
		w.str(binary.LittleEndian.String() + binary.BigEndian.String() + binary.NativeEndian.String())
		w.str(binary.LittleEndian.GoString() + binary.BigEndian.GoString() + binary.NativeEndian.GoString())
	case 1: // writes and appends
		o, ao := orders(v.h0)
		buf := make([]byte, 20)
		o.PutUint16(buf, uint16(v.x))
		o.PutUint32(buf[2:], uint32(v.x))
		o.PutUint64(buf[6:], v.y)
		binary.BigEndian.PutUint64(buf[12:], v.x)
		w.bs(buf)
		w.bs(ao.AppendUint16(fresh(v.b), uint16(v.y)))
		w.bs(ao.AppendUint32(nil, uint32(v.y)))
		w.bs(ao.AppendUint64(fresh(v.a), v.x))
		w.bs(binary.NativeEndian.AppendUint64(nil, v.y))
		binary.NativeEndian.PutUint32(buf, uint32(v.y))
		binary.LittleEndian.PutUint16(buf[4:], uint16(v.x>>3))
		w.bs(buf)
	case 2: // short buffers panic (the index of the last byte is out of range)
		o, ao := orders(v.h0)
		short := in[8 : 8+v.h1%9]
		w.bs(ao.AppendUint16(short[:0:0], 7))
		w.u8(0x42)
		switch v.h2 % 4 {
		case 0:
			w.u64(o.Uint64(short))
		case 1:
			o.PutUint32(short, uint32(v.x))
		case 2:
			w.u64(uint64(o.Uint16(short)))
		default:
			o.PutUint64(short, v.x)
		}
		w.bs(short)
	case 3: // varint decoding of arbitrary bytes, including overflows
		end := 8 + v.h0%14
		u, n := binary.Uvarint(in[8:end])
		w.u64(u)
		w.int(n)
		s, n := binary.Varint(in[8:end])
		w.u64(uint64(s))
		w.int(n)
		// Continuation bytes force long encodings.
		long := []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, byte(v.h1)}
		u, n = binary.Uvarint(long[v.h2%4:])
		w.u64(u)
		w.int(n)
		s, n = binary.Varint(long)
		w.u64(uint64(s))
		w.int(n)
	case 4: // varint encoding round trips
		buf := make([]byte, binary.MaxVarintLen64)
		n := binary.PutUvarint(buf, v.x>>(v.h0%64))
		w.bs(buf[:n])
		u, k := binary.Uvarint(buf[:n])
		w.u64(u)
		w.int(k)
		n = binary.PutVarint(buf, int64(v.y)>>(v.h1%64))
		w.bs(buf[:n])
		s, k := binary.Varint(buf[:n])
		w.u64(uint64(s))
		w.int(k)
		w.bs(binary.AppendUvarint(fresh(v.b), v.x))
		w.bs(binary.AppendVarint(nil, int64(v.y)))
		w.int(binary.MaxVarintLen16 + binary.MaxVarintLen32<<4 + binary.MaxVarintLen64<<8)
	case 5: // PutUvarint into a buffer that is too small panics
		buf := make([]byte, v.h0%11)
		w.u8(0x56)
		if v.h1&1 == 0 {
			w.int(binary.PutUvarint(buf, v.x))
		} else {
			w.int(binary.PutVarint(buf, int64(v.y)))
		}
		w.bs(buf)
	}
}
