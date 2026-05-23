// Copyright (C) MongoDB, Inc. 2024-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package mongo

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/internal/assert"
	"go.mongodb.org/mongo-driver/v2/internal/require"
	"go.mongodb.org/mongo-driver/v2/x/bsonx/bsoncore"
)

func TestBatches(t *testing.T) {
	t.Parallel()

	batches := &modelBatches{
		writePairs: make([]clientBulkWritePair, 2),
	}
	batches.AdvanceBatches(3)
	size := batches.Size()
	assert.Equal(t, 0, size, "expected: %d, got: %d", 1, size)
}

func TestAppendBatchSequence(t *testing.T) {
	t.Parallel()

	newBatches := func(t *testing.T) *modelBatches {
		client, err := newClient()
		require.NoError(t, err, "NewClient error: %v", err)
		return &modelBatches{
			client: client,
			writePairs: []clientBulkWritePair{
				{"ns0", nil},
				{"ns1", &ClientInsertOneModel{
					Document: bson.D{{"foo", 42}},
				}},
				{"ns2", &ClientReplaceOneModel{
					Filter:      bson.D{{"foo", "bar"}},
					Replacement: bson.D{{"foo", "baz"}},
				}},
				{"ns1", &ClientDeleteOneModel{
					Filter: bson.D{{"qux", "quux"}},
				}},
			},
			offset: 1,
			result: &ClientBulkWriteResult{
				Acknowledged: true,
			},
		}
	}
	t.Run("test appendBatches", func(t *testing.T) {
		t.Parallel()

		batches := newBatches(t)
		const limitBigEnough = 16_000
		n, _, err := batches.AppendBatchSequence(nil, 4, limitBigEnough)
		require.NoError(t, err, "AppendBatchSequence error: %v", err)
		require.Equal(t, 3, n, "expected %d appendings, got: %d", 3, n)

		_ = batches.cursorHandlers[0](&cursorInfo{Ok: true, Idx: 0}, nil)
		_ = batches.cursorHandlers[1](&cursorInfo{Ok: true, Idx: 1}, nil)
		_ = batches.cursorHandlers[2](&cursorInfo{Ok: true, Idx: 2}, nil)

		ins, ok := batches.result.InsertResults[1]
		assert.True(t, ok, "expected an insert results")
		assert.NotNil(t, ins.InsertedID, "expected an ID")

		_, ok = batches.result.UpdateResults[2]
		assert.True(t, ok, "expected an insert results")

		_, ok = batches.result.DeleteResults[3]
		assert.True(t, ok, "expected an delete results")
	})
	t.Run("test appendBatches with maxCount", func(t *testing.T) {
		t.Parallel()

		batches := newBatches(t)
		const limitBigEnough = 16_000
		n, _, err := batches.AppendBatchSequence(nil, 2, limitBigEnough)
		require.NoError(t, err, "AppendBatchSequence error: %v", err)
		require.Equal(t, 2, n, "expected %d appendings, got: %d", 2, n)

		_ = batches.cursorHandlers[0](&cursorInfo{Ok: true, Idx: 0}, nil)
		_ = batches.cursorHandlers[1](&cursorInfo{Ok: true, Idx: 1}, nil)

		ins, ok := batches.result.InsertResults[1]
		assert.True(t, ok, "expected an insert results")
		assert.NotNil(t, ins.InsertedID, "expected an ID")

		_, ok = batches.result.UpdateResults[2]
		assert.True(t, ok, "expected an insert results")

		_, ok = batches.result.DeleteResults[3]
		assert.False(t, ok, "expected an delete results")
	})
	t.Run("test nsInfoUUIDCallback", func(t *testing.T) {
		t.Parallel()

		uuid1 := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
		uuid2 := []byte{17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}

		decodeBatch := func(t *testing.T, batches *modelBatches) (ops, nsInfo []bson.Raw) {
			t.Helper()
			_, data, err := batches.AppendBatchArray(nil, 100, 16_000)
			require.NoError(t, err)
			idx, doc := bsoncore.AppendDocumentStart(nil)
			doc = append(doc, data...)
			doc, _ = bsoncore.AppendDocumentEnd(doc, idx)
			var result struct {
				Ops    []bson.Raw `bson:"ops"`
				NsInfo []bson.Raw `bson:"nsInfo"`
			}
			require.NoError(t, bson.Unmarshal(doc, &result))
			return result.Ops, result.NsInfo
		}

		nsIdxFromOp := func(t *testing.T, raw bson.Raw) int {
			t.Helper()
			var op struct {
				Insert *int32 `bson:"insert"`
			}
			require.NoError(t, bson.Unmarshal(raw, &op))
			require.NotNil(t, op.Insert)
			return int(*op.Insert)
		}

		decodeNsInfo := func(t *testing.T, raw bson.Raw) (ns string, uuid []byte) {
			t.Helper()
			var entry struct {
				Ns             string       `bson:"ns"`
				CollectionUUID *bson.Binary `bson:"collectionUUID"`
			}
			require.NoError(t, bson.Unmarshal(raw, &entry))
			if entry.CollectionUUID != nil {
				return entry.Ns, entry.CollectionUUID.Data
			}
			return entry.Ns, nil
		}

		newUUIDTestBatches := func(t *testing.T, pairs []clientBulkWritePair) *modelBatches {
			t.Helper()
			client, err := newClient()
			require.NoError(t, err)
			return &modelBatches{
				client:     client,
				writePairs: pairs,
				result:     &ClientBulkWriteResult{Acknowledged: true},
			}
		}

		t.Run("single namespace single UUID", func(t *testing.T) {
			t.Parallel()

			i := 0
			uuids := [][]byte{uuid1}
			batches := newUUIDTestBatches(t, []clientBulkWritePair{
				{"db.coll", &ClientInsertOneModel{Document: bson.D{{"x", 1}}}},
			})
			batches.nsInfoUUIDCallback = func(ns string) []byte {
				uuid := uuids[i]
				i++
				if i == len(uuids) {
					i = 0
				}
				return uuid
			}

			_, nsInfo := decodeBatch(t, batches)
			require.Len(t, nsInfo, 1)
			ns, uuid := decodeNsInfo(t, nsInfo[0])
			assert.Equal(t, "db.coll", ns)
			assert.Equal(t, uuid1, uuid)
		})

		t.Run("same namespace different UUIDs produces separate nsInfo entries", func(t *testing.T) {
			t.Parallel()

			i := 0
			uuids := [][]byte{uuid1, uuid2}
			batches := newUUIDTestBatches(t, []clientBulkWritePair{
				{"db.coll", &ClientInsertOneModel{Document: bson.D{{"x", 1}}}},
				{"db.coll", &ClientInsertOneModel{Document: bson.D{{"x", 2}}}},
			})
			batches.nsInfoUUIDCallback = func(ns string) []byte {
				uuid := uuids[i]
				i++
				if i == len(uuids) {
					i = 0
				}
				return uuid
			}

			ops, nsInfo := decodeBatch(t, batches)
			require.Len(t, nsInfo, 2)

			ns0, u0 := decodeNsInfo(t, nsInfo[0])
			assert.Equal(t, "db.coll", ns0)
			assert.Equal(t, uuid1, u0)

			ns1, u1 := decodeNsInfo(t, nsInfo[1])
			assert.Equal(t, "db.coll", ns1)
			assert.Equal(t, uuid2, u1)

			require.Len(t, ops, 2)
			assert.Equal(t, 0, nsIdxFromOp(t, ops[0]))
			assert.Equal(t, 1, nsIdxFromOp(t, ops[1]))
		})

		t.Run("no callback produces no collectionUUID", func(t *testing.T) {
			t.Parallel()

			batches := newUUIDTestBatches(t, []clientBulkWritePair{
				{"db.coll", &ClientInsertOneModel{Document: bson.D{{"x", 1}}}},
			})

			_, nsInfo := decodeBatch(t, batches)
			require.Len(t, nsInfo, 1)
			_, uuid := decodeNsInfo(t, nsInfo[0])
			assert.Nil(t, uuid)
		})
	})
	t.Run("test appendBatches with totalSize", func(t *testing.T) {
		t.Parallel()

		batches := newBatches(t)
		const limit = 1200 // > ( 166 first two batches + 1000 overhead )
		n, _, err := batches.AppendBatchSequence(nil, 4, limit)
		require.NoError(t, err, "AppendBatchSequence error: %v", err)
		require.Equal(t, 2, n, "expected %d appendings, got: %d", 2, n)

		_ = batches.cursorHandlers[0](&cursorInfo{Ok: true, Idx: 0}, nil)
		_ = batches.cursorHandlers[1](&cursorInfo{Ok: true, Idx: 1}, nil)

		ins, ok := batches.result.InsertResults[1]
		assert.True(t, ok, "expected an insert results")
		assert.NotNil(t, ins.InsertedID, "expected an ID")

		_, ok = batches.result.UpdateResults[2]
		assert.True(t, ok, "expected an insert results")

		_, ok = batches.result.DeleteResults[3]
		assert.False(t, ok, "expected an delete results")
	})
}
