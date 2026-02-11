/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package storage

import (
	"errors"
	"fmt"
	"os"
	"time"

	bolt "go.etcd.io/bbolt"
)

type BoltConfig struct {
	Path string // path to state file
	Sync bool   // should we flush after every single write
}

type BoltHandler struct {
	db *bolt.DB
}

// Verify checks that we have a good state file
func (s *BoltConfig) Verify() (err error) {
	// basic variable checks
	if s == nil {
		return errors.New("nil config")
	} else if s.Path == `` {
		return errors.New("missing state path")
	}

	//go attempt to open and close the state file
	var sh *BoltHandler
	if sh, err = OpenBoltHandler(s.Path, s.Sync); err != nil {
		err = fmt.Errorf("failed to open state file %w", err)
	} else if err = sh.Close(); err != nil {
		err = fmt.Errorf("failed to close state file %w", err)
	}
	return
}

func OpenBoltHandler(pth string, sync bool) (sh *BoltHandler, err error) {
	opt := bolt.Options{
		NoSync:  !sync,
		Timeout: time.Second,
	}

	info, err := os.Stat(pth)
	exists := !os.IsNotExist(err)
	if err != nil && exists { // if there is no file, bolt.Open will create it
		return
	}
	if exists && info.Mode().Perm()&0600 == 0 { // exists && !writable
		return nil, errors.New("existing state file is not writable")
	}
	var db *bolt.DB
	if db, err = bolt.Open(pth, 0600, &opt); err == nil {
		sh = &BoltHandler{
			db: db,
		}
	}
	return
}

func (sh *BoltHandler) check() (err error) {
	if sh == nil || sh.db == nil {
		err = errors.New("state handler not ready")
	}
	return
}

func (sh *BoltHandler) Close() (err error) {
	if err = sh.check(); err == nil {
		err = sh.db.Sync()
		if lerr := sh.db.Close(); lerr != nil && err == nil {
			err = lerr // only set the close error if sync succeeded and close did not
		}
		sh.db = nil
	}
	return
}

func (sh *BoltHandler) writeBucket(bucket, key, value []byte) (err error) {
	if len(bucket) == 0 {
		return errors.New("missing bucket")
	} else if len(key) == 0 {
		return errors.New("missing key")
	}
	if err = sh.check(); err == nil {
		err = sh.db.Update(func(tx *bolt.Tx) error {
			bkt, err := tx.CreateBucketIfNotExists(bucket)
			if err != nil {
				return err
			}
			return bkt.Put(key, value)
		})
	}
	return
}

// bucketReadHandler is a function prototype used for handling byte values coming back from
// bolt DB reads. The byte slice passed in should not be retained, as it is only valid during the
// lifetime of the function call.
type bucketReadHandler func([]byte) error

func (sh *BoltHandler) readBucket(bucket, key []byte, hnd bucketReadHandler) (err error) {
	if len(bucket) == 0 {
		err = errors.New("missing bucket")
		return
	} else if len(key) == 0 {
		err = errors.New("missing key")
		return
	} else if hnd == nil {
		err = errors.New("missing read handler")
		return
	}
	if err = sh.check(); err == nil {
		err = sh.db.View(func(tx *bolt.Tx) error {
			bkt := tx.Bucket(bucket)
			if bkt == nil {
				return ErrStorageNotFound
			}
			return hnd(bkt.Get(key))
		})
	}
	return
}

func (sh *BoltHandler) GetBucketWriter(bucket string) (bw *BucketWriter, err error) {
	if err = sh.check(); err == nil {
		b := []byte(bucket)
		err = sh.db.Update(func(tx *bolt.Tx) error {
			_, err := tx.CreateBucketIfNotExists(b)
			return err
		})
		if err == nil {
			bw = &BucketWriter{
				bucket: b,
				sh:     sh,
			}
		}
	}
	return
}

// BucketWriter implements the Storage interface for hosted ingesters
type BucketWriter struct {
	bucket []byte
	sh     *BoltHandler
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
		err = bw.sh.readBucket(bw.bucket, []byte(key), func(v []byte) error {
			if v == nil {
				return ErrStorageNotFound
			}
			value = retslice(v)
			return nil
		})
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
	if err = bw.check(); err == nil {
		err = bw.sh.readBucket(bw.bucket, []byte(key), func(v []byte) error {
			if v == nil {
				return ErrStorageNotFound
			} else if len(v) > 0 {
				value = string(v) // the string cast will perform the copy for us
			}
			return nil
		})
	}
	return
}

// PutString puts a native string into the storage
func (bw *BucketWriter) PutString(key, value string) (err error) {
	err = bw.Put(key, []byte(value))
	return
}

// GetInt64 pulls back a native int64 from state storage
func (bw *BucketWriter) GetInt64(key string) (value int64, err error) {
	if err = bw.check(); err == nil {
		err = bw.sh.readBucket(bw.bucket, []byte(key), func(v []byte) error {
			if len(v) == 0 {
				return ErrStorageNotFound
			}
			var lerr error
			var n int
			if n, lerr = fmt.Sscanf(string(v), "%d", &value); lerr == nil && n != 1 {
				lerr = fmt.Errorf("failed to parse int64 from %d scanned items", n)
			}
			return lerr
		})
	}
	return
}

// PutInt64 puts a native int64 into the storage
func (bw *BucketWriter) PutInt64(key string, value int64) (err error) {
	err = bw.Put(key, []byte(fmt.Sprintf("%d", value)))
	return
}

// GetTime pulls back a native time.Time from state storage
func (bw *BucketWriter) GetTime(key string) (value time.Time, err error) {
	// we just wrap the native slice getter, no special encoding here
	if err = bw.check(); err == nil {
		err = bw.sh.readBucket(bw.bucket, []byte(key), func(v []byte) error {
			if len(v) == 0 {
				return ErrStorageNotFound
			}
			var lerr error
			value, lerr = time.Parse(time.RFC3339Nano, string(v))
			return lerr
		})
	}
	return
}

// PutTime puts a native time.Time into the storage
func (bw *BucketWriter) PutTime(key string, value time.Time) (err error) {
	err = bw.Put(key, []byte(value.Format(time.RFC3339Nano)))
	return
}

// func retslice is just a wrapper around a make and copy so that we can safely return slices from bolt Views
func retslice(v []byte) (r []byte) {
	if len(v) > 0 {
		r = make([]byte, len(v))
		copy(r, v)
	}
	return
}
