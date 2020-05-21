package chancacher

import "os"

type fileCounter struct {
	*os.File
	count int
}

func NewFileCounter(f *os.File) (*fileCounter, error) {
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return &fileCounter{
		File:  f,
		count: int(fi.Size()),
	}, nil
}

func (f *fileCounter) Write(b []byte) (n int, err error) {
	f.count += len(b)
	return f.File.Write(b)
}

func (f *fileCounter) Read(b []byte) (n int, err error) {
	n, err = f.File.Read(b)
	f.count -= n
	return
}

func (f *fileCounter) Count() int {
	return f.count
}
