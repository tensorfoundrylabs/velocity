package velocity

import (
	"bytes"
	"sync"
)

// templateBufferPool manages bytes.Buffer instances for template operations.
var templateBufferPool = sync.Pool{
	New: func() any {
		return bytes.NewBuffer(make([]byte, 0, 512))
	},
}

func GetTemplateBuffer() *bytes.Buffer {
	buf, ok := templateBufferPool.Get().(*bytes.Buffer)
	if !ok || buf == nil {
		// Fallback in case pool returns unexpected type
		buf = bytes.NewBuffer(make([]byte, 0, 512))
	}
	buf.Reset()
	return buf
}

func PutTemplateBuffer(buf *bytes.Buffer) {
	if buf == nil {
		return
	}
	// Don't pool buffers that grew too large
	if buf.Cap() > 4096 {
		return
	}
	templateBufferPool.Put(buf)
}

// fieldPool manages pre-allocated field slices.
// Typical logs have 8 or fewer fields, so we pre-allocate this capacity.
type fieldPool struct {
	pool sync.Pool
}

var fieldSlicePool = &fieldPool{
	pool: sync.Pool{
		New: func() any {
			slice := make([]Field, 0, 8)
			return &slice
		},
	},
}

func GetFieldSlice() []Field {
	slicePtr, ok := fieldSlicePool.pool.Get().(*[]Field)
	if !ok || slicePtr == nil {
		// Fallback in case pool returns unexpected type
		slice := make([]Field, 0, 8)
		return slice
	}
	*slicePtr = (*slicePtr)[:0]
	return *slicePtr
}

func GetFieldSliceWithCapacity(capacity int) []Field {
	slicePtr, ok := fieldSlicePool.pool.Get().(*[]Field)
	if !ok || slicePtr == nil {
		// Fallback in case pool returns unexpected type
		return make([]Field, 0, capacity)
	}
	*slicePtr = (*slicePtr)[:0]

	if cap(*slicePtr) < capacity {
		*slicePtr = make([]Field, 0, capacity)
	}

	return *slicePtr
}

// PutFieldSlice returns a field slice to the pool for reuse.
// The slice is reset to avoid data leakage.
func PutFieldSlice(fields []Field) {
	if fields == nil {
		return
	}

	// Don't pool slices that grew too large
	if cap(fields) > 64 {
		return
	}

	fields = fields[:0]
	fieldSlicePool.pool.Put(&fields)
}
