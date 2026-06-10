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
	result := (*slicePtr)[:0]
	// Return the wrapper to slicePtrPool so PutFieldSlice can reuse it and
	// avoid a per-call allocation when putting the slice back.
	slicePtrPool.Put(slicePtr)
	return result
}

func GetFieldSliceWithCapacity(capacity int) []Field {
	slicePtr, ok := fieldSlicePool.pool.Get().(*[]Field)
	if !ok || slicePtr == nil {
		// Fallback in case pool returns unexpected type
		return make([]Field, 0, capacity)
	}
	var result []Field
	if cap(*slicePtr) < capacity {
		result = make([]Field, 0, capacity)
	} else {
		result = (*slicePtr)[:0]
	}
	slicePtrPool.Put(slicePtr)
	return result
}

// slicePtrPool is a secondary pool for the *[]Field wrapper objects themselves.
// Putting &fields (address of a local parameter) into fieldSlicePool.pool causes
// a heap allocation per call because the local copy escapes. Instead, we recycle
// the wrapper pointer from this pool so PutFieldSlice doesn't allocate.
var slicePtrPool = sync.Pool{
	New: func() any {
		s := make([]Field, 0, 8)
		return &s
	},
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

	// Borrow a *[]Field wrapper from slicePtrPool, store the slice into it, and
	// put the wrapper into fieldSlicePool. This avoids the per-call heap allocation
	// that &fields (address of a local parameter copy) would cause.
	p, _ := slicePtrPool.Get().(*[]Field)
	if p == nil {
		s := make([]Field, 0, cap(fields))
		p = &s
	}
	*p = fields[:0]
	fieldSlicePool.pool.Put(p)
}
