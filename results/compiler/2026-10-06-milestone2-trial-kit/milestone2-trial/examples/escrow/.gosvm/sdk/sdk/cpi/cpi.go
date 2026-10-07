// Package cpi provides bounded account-meta and PDA-signer builders.
package cpi

import (
	"gosvm/sdk/pda"
	"gosvm/solana"
)

const ErrMetas uint64 = 3002
const ErrSigners uint64 = 3003

type Metas struct {
	buffer [144]byte
	count  uint64
}

func (m *Metas) Add(index uint64, writable, signer bool) uint64 {
	if m.count >= 16 {
		return ErrMetas
	}
	for j := uint64(0); j < 8; j++ {
		m.buffer[m.count*9+j] = byte(index >> (j * 8))
	}
	flags := byte(0)
	if writable {
		flags = flags | 1
	}
	if signer {
		flags = flags | 2
	}
	m.buffer[m.count*9+8] = flags
	m.count++
	return 0
}

func (m *Metas) Bytes() []byte { return m.buffer[:m.count*9] }

type Signers struct {
	buffer [1024]byte
	used   uint64
	count  uint64
}

func (s *Signers) Add(seeds *pda.Seeds) uint64 {
	if seeds == nil || s.count >= 2 || s.used > 1023 {
		return ErrSigners
	}
	b := seeds.Bytes()
	if uint64(len(b)) > 1023-s.used {
		return ErrSigners
	}
	for j := uint64(0); j < uint64(len(b)); j++ {
		s.buffer[1+s.used+j] = b[j]
	}
	s.used = s.used + uint64(len(b))
	s.count++
	s.buffer[0] = byte(s.count)
	return 0
}

func (s *Signers) Bytes() []byte { return s.buffer[:1+s.used] }

func Invoke(c solana.Context, program uint64, metas *Metas, data []byte, signers *Signers) uint64 {
	if metas == nil {
		return ErrMetas
	}
	if signers == nil {
		return solana.Invoke(c, program, metas.Bytes(), data, nil)
	}
	return solana.Invoke(c, program, metas.Bytes(), data, signers.Bytes())
}
