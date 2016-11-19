package io

import (
	"errors"
	"hash"
	"hash/fnv"
	"io"
	"v2ray.com/core/common/alloc"
	"v2ray.com/core/common/serial"
)

// Private: Visible for testing.
type Validator struct {
	actualAuth   hash.Hash32
	expectedAuth uint32
}

func NewValidator(expectedAuth uint32) *Validator {
	return &Validator{
		actualAuth:   fnv.New32a(),
		expectedAuth: expectedAuth,
	}
}

func (this *Validator) Consume(b []byte) {
	this.actualAuth.Write(b)
}

func (this *Validator) Validate() bool {
	return this.actualAuth.Sum32() == this.expectedAuth
}

type AuthChunkReader struct {
	reader      io.Reader
	last        *alloc.Buffer
	chunkLength int
	validator   *Validator
}

func NewAuthChunkReader(reader io.Reader) *AuthChunkReader {
	return &AuthChunkReader{
		reader:      reader,
		chunkLength: -1,
	}
}

func (this *AuthChunkReader) Read() (*alloc.Buffer, error) {
	var buffer *alloc.Buffer
	if this.last != nil {
		buffer = this.last
		this.last = nil
	} else {
		buffer = alloc.NewBuffer().Clear()
	}

	if this.chunkLength == -1 {
		for buffer.Len() < 6 {
			_, err := buffer.FillFrom(this.reader)
			if err != nil {
				buffer.Release()
				return nil, io.ErrUnexpectedEOF
			}
		}
		length := serial.BytesToUint16(buffer.Value[:2])
		this.chunkLength = int(length) - 4
		this.validator = NewValidator(serial.BytesToUint32(buffer.Value[2:6]))
		buffer.SliceFrom(6)
		if buffer.Len() < this.chunkLength && this.chunkLength <= 2048 {
			_, err := buffer.FillFrom(this.reader)
			if err != nil {
				buffer.Release()
				return nil, io.ErrUnexpectedEOF
			}
		}
	} else if buffer.Len() < this.chunkLength {
		_, err := buffer.FillFrom(this.reader)
		if err != nil {
			buffer.Release()
			return nil, io.ErrUnexpectedEOF
		}
	}

	if this.chunkLength == 0 {
		buffer.Release()
		return nil, io.EOF
	}

	if buffer.Len() < this.chunkLength {
		this.validator.Consume(buffer.Value)
		this.chunkLength -= buffer.Len()
	} else {
		this.validator.Consume(buffer.Value[:this.chunkLength])
		if !this.validator.Validate() {
			buffer.Release()
			return nil, errors.New("VMess|AuthChunkReader: Invalid auth.")
		}
		leftLength := buffer.Len() - this.chunkLength
		if leftLength > 0 {
			this.last = alloc.NewBuffer().Clear()
			this.last.Append(buffer.Value[this.chunkLength:])
			buffer.Slice(0, this.chunkLength)
		}

		this.chunkLength = -1
		this.validator = nil
	}

	return buffer, nil
}

func (this *AuthChunkReader) Release() {
	this.reader = nil
	this.last.Release()
	this.last = nil
	this.validator = nil
}
