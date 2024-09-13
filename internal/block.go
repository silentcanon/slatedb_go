package internal

import (
	"bytes"
	"encoding/binary"
	"github.com/samber/mo"
	"math"
)

const (
	// uint16 and uint32 sizes are constant as per https://go.dev/ref/spec#Size_and_alignment_guarantees

	SizeOfUint16InBytes = 2
	SizeOfUint32InBytes = 4

	Tombstone = math.MaxUint32
)

// ------------------------------------------------
// Block
// ------------------------------------------------

type Block struct {
	data    []byte
	offsets []uint16
}

// encode converts Block to a byte slice
// data is added to the first len(data) bytes
// offsets are added to the next len(offsets) * SizeOfUint16InBytes bytes
// the last 2 bytes hold the number of offsets
func (b *Block) encode() []byte {
	bufSize := len(b.data) + len(b.offsets)*SizeOfUint16InBytes + SizeOfUint16InBytes

	buf := make([]byte, 0, bufSize)
	buf = append(buf, b.data...)

	for _, offset := range b.offsets {
		buf = binary.BigEndian.AppendUint16(buf, offset)
	}
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(b.offsets)))
	return buf
}

// decode converts byte slice to a Block
func decodeBytesToBlock(bytes []byte) Block {
	// the last 2 bytes hold the number of offsets
	offsetCountIndex := len(bytes) - SizeOfUint16InBytes
	offsetCount := binary.BigEndian.Uint16(bytes[offsetCountIndex:])

	offsetStartIndex := offsetCountIndex - (int(offsetCount) * SizeOfUint16InBytes)
	offsets := make([]uint16, 0, offsetCount)

	for i := 0; i < int(offsetCount); i++ {
		index := offsetStartIndex + (i * SizeOfUint16InBytes)
		offsets = append(offsets, binary.BigEndian.Uint16(bytes[index:]))
	}

	return Block{
		data:    bytes[:offsetStartIndex],
		offsets: offsets,
	}
}

// ------------------------------------------------
// BlockBuilder
// ------------------------------------------------

type BlockBuilder struct {
	offsets   []uint16
	data      []byte
	blockSize uint
}

func NewBlockBuilder(blockSize uint) BlockBuilder {
	return BlockBuilder{
		offsets:   make([]uint16, 0),
		data:      make([]byte, 0),
		blockSize: blockSize,
	}
}

func (b *BlockBuilder) estimatedSize() int {
	return SizeOfUint16InBytes + // number of key-value pairs in the block
		(len(b.offsets) * SizeOfUint16InBytes) + // offsets
		len(b.data) // key-value pairs
}

func (b *BlockBuilder) add(key []byte, value mo.Option[[]byte]) bool {
	if len(key) == 0 {
		panic("key must not be empty")
	}

	valueLen := 0
	val, ok := value.Get()
	if ok {
		valueLen = len(val)
	}
	newSize := b.estimatedSize() + len(key) + valueLen + (SizeOfUint16InBytes * 2) + SizeOfUint32InBytes

	// If adding the key-value pair would exceed the block size limit, don't add it.
	// (Unless the block is empty, in which case, allow the block to exceed the limit.)
	if uint(newSize) > b.blockSize && !b.isEmpty() {
		return false
	}

	b.offsets = append(b.offsets, uint16(len(b.data)))

	// If value is present then append KeyLength(uint16), Key, ValueLength(uint32), value.
	// if value is absent then append KeyLength(uint16), Key, Tombstone(uint32)
	b.data = binary.BigEndian.AppendUint16(b.data, uint16(len(key)))
	b.data = append(b.data, key...)
	if valueLen > 0 {
		b.data = binary.BigEndian.AppendUint32(b.data, uint32(valueLen))
		b.data = append(b.data, val...)
	} else {
		b.data = binary.BigEndian.AppendUint32(b.data, Tombstone)
	}

	return true
}

func (b *BlockBuilder) isEmpty() bool {
	return len(b.offsets) == 0
}

func (b *BlockBuilder) build() (*Block, error) {
	if b.isEmpty() {
		return nil, EmptyBlock
	}
	return &Block{
		data:    b.data,
		offsets: b.offsets,
	}, nil
}

// ------------------------------------------------
// BlockIterator
// ------------------------------------------------

type BlockIterator struct {
	block       *Block
	offsetIndex uint
}

// newBlockIteratorFromKey Construct a BlockIterator that starts at the given key, or at the first
// key greater than the given key if the exact key given is not in the block.
func newBlockIteratorFromKey(block *Block, key []byte) *BlockIterator {
	data := block.data
	index := len(block.offsets)
	// TODO: Rust implementation uses partition_point() which internally uses binary search
	//  we are doing linear search. See if we can optimize
	for i, offset := range block.offsets {
		off := offset
		keyLen := binary.BigEndian.Uint16(data[off:])
		off += SizeOfUint16InBytes
		curKey := data[off : off+keyLen]
		if bytes.Compare(curKey, key) >= 0 {
			index = i
			break
		}
	}
	return &BlockIterator{
		block:       block,
		offsetIndex: uint(index),
	}
}

func newBlockIteratorFromFirstKey(block *Block) *BlockIterator {
	return &BlockIterator{
		block:       block,
		offsetIndex: 0,
	}
}

func (b *BlockIterator) Next() mo.Option[KeyValue] {
	for {
		keyVal, ok := b.NextEntry().Get()
		if ok {
			if keyVal.valueDel.isTombstone {
				continue
			}

			return mo.Some[KeyValue](KeyValue{
				key:   keyVal.key,
				value: keyVal.valueDel.value,
			})
		} else {
			return mo.None[KeyValue]()
		}
	}
}

func (b *BlockIterator) NextEntry() mo.Option[KeyValueDeletable] {
	keyValue, ok := b.loadAtCurrentOffset().Get()
	if !ok {
		return mo.None[KeyValueDeletable]()
	}

	b.advance()
	return mo.Some(keyValue)
}

func (b *BlockIterator) advance() {
	b.offsetIndex += 1
}

func (b *BlockIterator) loadAtCurrentOffset() mo.Option[KeyValueDeletable] {
	if b.offsetIndex >= uint(len(b.block.offsets)) {
		return mo.None[KeyValueDeletable]()
	}

	data := b.block.data
	offset := b.block.offsets[b.offsetIndex]
	var valueDel ValueDeletable

	// Read KeyLength(uint16), Key, (ValueLength(uint32), value)/Tombstone(uint32) from data
	keyLen := binary.BigEndian.Uint16(data[offset:])
	offset += SizeOfUint16InBytes

	key := data[offset : offset+keyLen]
	offset += keyLen

	valueLen := binary.BigEndian.Uint32(data[offset:])
	offset += SizeOfUint32InBytes

	if valueLen != Tombstone {
		valueDel = ValueDeletable{
			value:       data[offset : uint32(offset)+valueLen],
			isTombstone: false,
		}
	} else {
		valueDel = ValueDeletable{
			value:       nil,
			isTombstone: true,
		}
	}

	return mo.Some(KeyValueDeletable{
		key:      key,
		valueDel: valueDel,
	})
}
