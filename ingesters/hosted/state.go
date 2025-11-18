/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package hosted

import (
	"errors"
	"fmt"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

type StateConfig struct {
	Path string //path to state file
	Sync bool   // should we flush after every single write
}

type StateHandler struct {
	sync.Mutex
	db *bolt.DB
}

// Verify checks that we have a good state file
func (s *StateConfig) Verify() (err error) {

	// basic variable checks
	if s == nil {
		return errors.New("nil state")
	} else if s.Path == `` {
		return errors.New("missing state path")
	}

	//go attempt to open and close the state file
	var sh *StateHandler
	if sh, err = OpenStateHandler(s.Path, s.Sync); err != nil {
		err = fmt.Errorf("failed to open state file %w", err)
	} else if err = sh.Close(); err != nil {
		err = fmt.Errorf("failed to close state file %w", err)
	}
	return
}

func OpenStateHandler(pth string, sync bool) (sh *StateHandler, err error) {
	opt := bolt.Options{
		NoSync:  !sync,
		Timeout: time.Second,
	}
	var db *bolt.DB
	if db, err = bolt.Open(pth, 0600, &opt); err == nil {
		if db.IsReadOnly() {
			err = errors.New("state file is readonly")
			return
		}
		sh = &StateHandler{
			db: db,
		}
	}
	return
}

func (sh *StateHandler) check() (err error) {
	if sh == nil || sh.db == nil {
		err = errors.New("state handler not ready")
	}
	return
}

func (sh *StateHandler) Close() (err error) {
	if err = sh.check(); err == nil {
		sh.Lock()
		err = sh.db.Sync()
		if lerr := sh.db.Close(); lerr != nil && err == nil {
			err = lerr // only set the close error if sync succeeded and close did not
		}
		sh.db = nil
		sh.Unlock()
	}
	return
}

func (sh *StateHandler) writeBucket(bucket, key, value []byte) (err error) {
	if len(bucket) == 0 {
		return errors.New("missing bucket")
	} else if len(key) == 0 {
		return errors.New("missing key")
	}
	if err = sh.check(); err == nil {
		sh.Lock()
		err = sh.db.Update(func(tx *bolt.Tx) error {
			bkt, err := tx.CreateBucketIfNotExists(bucket)
			if err != nil {
				return err
			}
			return bkt.Put(key, value)
		})
		sh.Unlock()
	}
	return
}

func (sh *StateHandler) readBucket(bucket, key []byte) (value []byte, err error) {
	if len(bucket) == 0 {
		err = errors.New("missing bucket")
		return
	} else if len(key) == 0 {
		err = errors.New("missing key")
		return
	}
	if err = sh.check(); err == nil {
		sh.Lock()
		err = sh.db.View(func(tx *bolt.Tx) error {
			bkt, err := tx.CreateBucketIfNotExists(bucket)
			if err != nil {
				return err
			}
			return bkt.Put(key, value)
		})
		sh.Unlock()
	}
	return
}

func (sh *StateHandler) getBucketWriter(bucket string) (bw *BucketWriter, err error) {
	if err = sh.check(); err == nil {
		b := []byte(bucket)
		sh.Lock()
		err = sh.db.Update(func(tx *bolt.Tx) error {
			_, err := tx.CreateBucketIfNotExists(b)
			return err
		})
		sh.Unlock()
		if err == nil {
			bw = &BucketWriter{
				bucket: b,
				sh:     sh,
			}
		}
	}
	return
}

// BucketWriter implements the Storage inteface for hosted ingesters
type BucketWriter struct {
	bucket []byte
	sh     *StateHandler
}

func (bw *BucketWriter) check() (err error) {
	if bw == nil || bw.sh == nil {
		err = errors.New("bucket writer not ready")
	}
	return
}

// Get pulls back a native byte slice from state storage
func (bw *BucketWriter) Get(key string) (value []byte, err error) {
	if err = bw.check(); err == nil {
		value, err = bw.sh.readBucket(bw.bucket, []byte(key))
	}
	return
}

// Put puts a native byte slice into the storage
func (bw *BucketWriter) Put(key string, value []byte) (err error) {
	if err = bw.check(); err == nil {
		err = bw.sh.writeBucket(bw.bucket, []byte(key), value)
	}
	return

}

// GetString pulls back a native string from state storage
func (bw *BucketWriter) GetString(key string) (value string, err error) {
	// we just wrap the native slice getter, no special encoding here
	var bv []byte
	if bv, err = bw.Get(key); err == nil && len(bv) > 0 {
		value = string(bv)
	}
	return
}

// PutString puts a native string into the storage
func (bw *BucketWriter) PutString(key, value string) (err error) {
	err = bw.Put(key, []byte(value))
	return
}

// GetTime pulls back a native time.Time from state storage
func (bw *BucketWriter) GetTime(key string) (value time.Time, err error) {
	// we just wrap the native slice getter, no special encoding here
	var bv []byte
	if bv, err = bw.Get(key); err == nil && len(bv) > 0 {
		value, err = time.Parse(time.RFC3339Nano, string(bv))
	}
	return
}

// PutTime puts a native time.Time into the storage
func (bw *BucketWriter) PutTime(key string, value time.Time) (err error) {
	err = bw.Put(key, []byte(value.Format(time.RFC3339Nano)))
	return
}
