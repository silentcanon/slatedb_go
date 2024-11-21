package table

import (
	"bytes"
	"github.com/slatedb/slatedb-go/slatedb/common"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestMemtableOps(t *testing.T) {
	kvPairs := []common.KV{
		{Key: []byte("abc111"), Value: []byte("value1")},
		{Key: []byte("abc222"), Value: []byte("value2")},
		{Key: []byte("abc333"), Value: []byte("value3")},
	}

	memtable := NewMemtable()

	var size int64
	// Put KV pairs and verify Get
	for _, kvPair := range kvPairs {
		size += memtable.Put(kvPair.Key, kvPair.Value)
	}
	for _, kvPair := range kvPairs {
		assert.Equal(t, kvPair.Value, memtable.Get(kvPair.Key).MustGet().Value)
	}
	assert.Equal(t, size, memtable.Size())

	// Delete KV and verify that it is tombstoned
	memtable.Delete(kvPairs[1].Key)
	assert.True(t, memtable.Get(kvPairs[1].Key).MustGet().IsTombstone)

	memtable.SetLastWalID(1)
	assert.Equal(t, uint64(1), memtable.LastWalID().MustGet())
}

func TestMemtableIter(t *testing.T) {
	kvPairs := []common.KV{
		{Key: []byte("abc111"), Value: []byte("value1")},
		{Key: []byte("abc222"), Value: []byte("value2")},
		{Key: []byte("abc333"), Value: []byte("value3")},
		{Key: []byte("abc444"), Value: []byte("value4")},
		{Key: []byte("abc555"), Value: []byte("value5")},
	}

	memtable := NewMemtable()

	// Put keys in random order
	indexes := []int{2, 0, 4, 3, 1}
	for i := range indexes {
		memtable.Put(kvPairs[i].Key, kvPairs[i].Value)
	}

	iter := memtable.Iter()

	// Verify that iterator returns keys in sorted order
	for i := 0; i < len(kvPairs); i++ {
		next, err := iter.Next()
		assert.NoError(t, err)
		kv, ok := next.Get()
		assert.True(t, ok)
		assert.Equal(t, kvPairs[i].Key, kv.Key)
		assert.Equal(t, kvPairs[i].Value, kv.Value)
	}
}

func TestMemtableIterDelete(t *testing.T) {
	memtable := NewMemtable()

	memtable.Put([]byte("abc333"), []byte("value3"))
	next, err := memtable.Iter().Next()
	assert.NoError(t, err)
	assert.True(t, next.IsPresent())

	memtable.Delete([]byte("abc333"))
	next, err = memtable.Iter().Next()
	assert.NoError(t, err)
	assert.False(t, next.IsPresent())
}

func TestMemtableRangeFromExistingKey(t *testing.T) {
	kvPairs := []common.KV{
		{Key: []byte("abc111"), Value: []byte("value1")},
		{Key: []byte("abc222"), Value: []byte("value2")},
		{Key: []byte("abc333"), Value: []byte("value3")},
		{Key: []byte("abc444"), Value: []byte("value4")},
		{Key: []byte("abc555"), Value: []byte("value5")},
	}

	memtable := NewMemtable()

	// Put keys in random order
	indexes := []int{2, 0, 4, 3, 1}
	for i := range indexes {
		memtable.Put(kvPairs[i].Key, kvPairs[i].Value)
	}

	iter := memtable.RangeFrom([]byte("abc333"))

	// Verify that iterator starts from index 2 which contains key abc333
	for i := 2; i < len(kvPairs); i++ {
		next, err := iter.Next()
		assert.NoError(t, err)
		kv, ok := next.Get()
		assert.True(t, ok)
		assert.Equal(t, kvPairs[i].Key, kv.Key)
		assert.Equal(t, kvPairs[i].Value, kv.Value)
	}
}

func TestMemtableRangeFromNonExistingKey(t *testing.T) {
	kvPairs := []common.KV{
		{Key: []byte("abc111"), Value: []byte("value1")},
		{Key: []byte("abc222"), Value: []byte("value2")},
		{Key: []byte("abc333"), Value: []byte("value3")},
		{Key: []byte("abc444"), Value: []byte("value4")},
		{Key: []byte("abc555"), Value: []byte("value5")},
	}

	memtable := NewMemtable()

	// Put keys in random order
	indexes := []int{2, 0, 4, 3, 1}
	for i := range indexes {
		memtable.Put(kvPairs[i].Key, kvPairs[i].Value)
	}

	iter := memtable.RangeFrom([]byte("abc345"))

	// Verify that iterator starts from index 3 which contains key abc444
	for i := 3; i < len(kvPairs); i++ {
		next, err := iter.Next()
		assert.NoError(t, err)
		kv, ok := next.Get()
		assert.True(t, ok)
		assert.Equal(t, kvPairs[i].Key, kv.Key)
		assert.Equal(t, kvPairs[i].Value, kv.Value)
	}
}

func TestImmMemtableOps(t *testing.T) {
	kvPairs := []common.KV{
		{Key: []byte("abc111"), Value: []byte("value1")},
		{Key: []byte("abc222"), Value: []byte("value2")},
		{Key: []byte("abc333"), Value: []byte("value3")},
	}

	memtable := NewMemtable()
	// Put keys in random order
	indexes := []int{1, 2, 0}
	for i := range indexes {
		memtable.Put(kvPairs[i].Key, kvPairs[i].Value)
	}

	// create ImmutableMemtable from memtable and verify Get
	immMemtable := NewImmutableMemtable(memtable, 1)
	for _, kvPair := range kvPairs {
		assert.Equal(t, kvPair.Value, immMemtable.Get(kvPair.Key).MustGet().Value)
	}
	assert.Equal(t, uint64(1), immMemtable.LastWalID())

	iter := immMemtable.Iter()

	// Verify that iterator returns keys in sorted order
	for i := 0; i < len(kvPairs); i++ {
		next, err := iter.Next()
		assert.NoError(t, err)
		kv, ok := next.Get()
		assert.True(t, ok)
		assert.Equal(t, kvPairs[i].Key, kv.Key)
		assert.Equal(t, kvPairs[i].Value, kv.Value)
	}
}

func TestMemtableClone(t *testing.T) {
	kvPairs := []common.KV{
		{Key: []byte("abc111"), Value: []byte("value1")},
		{Key: []byte("abc222"), Value: []byte("value2")},
		{Key: []byte("abc333"), Value: []byte("value3")},
	}

	memtable := NewMemtable()
	// Put KV pairs to memtable
	for _, kvPair := range kvPairs {
		memtable.Put(kvPair.Key, kvPair.Value)
	}
	memtable.SetLastWalID(1)

	clonedMemtable := memtable.Clone()
	// verify that they do not point to same data in memory but the contents are equal
	assert.NotEqual(t, memtable.table, clonedMemtable.table)
	assert.Equal(t, memtable.LastWalID(), clonedMemtable.LastWalID())
	assert.True(t, bytes.Equal(memtable.table.toBytes(), clonedMemtable.table.toBytes()))

	immMemtable := NewImmutableMemtable(memtable, 1)
	clonedImmMemtable := immMemtable.Clone()
	// verify that they do not point to same data in memory but the contents are equal
	assert.NotEqual(t, immMemtable.table, clonedImmMemtable.table)
	assert.Equal(t, immMemtable.LastWalID(), clonedImmMemtable.LastWalID())
	assert.True(t, bytes.Equal(immMemtable.table.toBytes(), clonedImmMemtable.table.toBytes()))
}
