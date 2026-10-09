package target

import (
	"encoding/binary"
	"testing"
)

func TestVersionAndMachineAreBothRequired(t *testing.T) {
	image := make([]byte, 64)
	copy(image, "\x7fELF")
	image[4] = 2
	image[5] = 1
	binary.LittleEndian.PutUint16(image[16:18], 3)
	legacy, _ := Resolve(Legacy)
	fogo, _ := Resolve(Fogo)
	modern, _ := Resolve(Solana)
	for _, p := range []Profile{legacy, fogo, modern} {
		machine := uint16(263)
		if p.Name == Solana {
			machine = 247
		}
		binary.LittleEndian.PutUint16(image[18:20], machine)
		binary.LittleEndian.PutUint32(image[48:52], p.Flags)
		if err := p.ValidateProgram(image); err != nil {
			t.Fatal(p.Name, err)
		}
		binary.LittleEndian.PutUint32(image[48:52], 99)
		if err := p.ValidateProgram(image); err == nil {
			t.Fatal("accepted incompatible ISA")
		}
	}
	// Both are flagged v3, but legacy experimental ELF is not modern Solana v3.
	binary.LittleEndian.PutUint16(image[18:20], 263)
	binary.LittleEndian.PutUint32(image[48:52], 3)
	if err := modern.ValidateProgram(image); err == nil {
		t.Fatal("reinterpreted historical v3 ELF")
	}
}
