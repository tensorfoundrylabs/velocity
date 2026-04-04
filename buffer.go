package velocity

import (
	"bytes"
	"sync"
	"time"
	"unsafe"
)

// BufferPool manages byte buffers of various sizes for efficient logging output.
// Using a tiered pool reduces garbage collection pressure during high-throughput logging.
type BufferPool struct {
	small  sync.Pool
	medium sync.Pool
	large  sync.Pool
	xlarge sync.Pool
}

const (
	// Buffer sizes optimised based on typical log message patterns.
	// Sizes are powers of 2 for efficient memory alignment.
	bufSmallSize  = 512
	bufMediumSize = 2048
	bufLargeSize  = 8192
	bufXLargeSize = 32768

	HintConsoleLog    = 256
	HintStructuredLog = 1024
	HintStackTrace    = 4096
)

var globalBufferPool = NewBufferPool()

func NewBufferPool() *BufferPool {
	return &BufferPool{
		small: sync.Pool{
			New: func() any {
				return bytes.NewBuffer(make([]byte, 0, bufSmallSize))
			},
		},
		medium: sync.Pool{
			New: func() any {
				return bytes.NewBuffer(make([]byte, 0, bufMediumSize))
			},
		},
		large: sync.Pool{
			New: func() any {
				return bytes.NewBuffer(make([]byte, 0, bufLargeSize))
			},
		},
		xlarge: sync.Pool{
			New: func() any {
				return bytes.NewBuffer(make([]byte, 0, bufXLargeSize))
			},
		},
	}
}

// Get retrieves a buffer from the pool, selecting the size based on the hint.
func (bp *BufferPool) Get(hint int) *bytes.Buffer {
	var buf *bytes.Buffer

	switch {
	case hint <= 0:
		if b, ok := bp.small.Get().(*bytes.Buffer); ok {
			buf = b
		} else {
			buf = bytes.NewBuffer(make([]byte, 0, bufSmallSize))
		}
	case hint <= bufSmallSize:
		if b, ok := bp.small.Get().(*bytes.Buffer); ok {
			buf = b
		} else {
			buf = bytes.NewBuffer(make([]byte, 0, bufSmallSize))
		}
	case hint <= bufMediumSize:
		if b, ok := bp.medium.Get().(*bytes.Buffer); ok {
			buf = b
		} else {
			buf = bytes.NewBuffer(make([]byte, 0, bufMediumSize))
		}
	case hint <= bufLargeSize:
		if b, ok := bp.large.Get().(*bytes.Buffer); ok {
			buf = b
		} else {
			buf = bytes.NewBuffer(make([]byte, 0, bufLargeSize))
		}
	default:
		if b, ok := bp.xlarge.Get().(*bytes.Buffer); ok {
			buf = b
		} else {
			buf = bytes.NewBuffer(make([]byte, 0, bufXLargeSize))
		}
	}

	buf.Reset()
	return buf
}

// Put returns a buffer to the appropriate pool based on its capacity.
// Buffers are not resized to preserve their grown capacity for reuse.
func (bp *BufferPool) Put(buf *bytes.Buffer) {
	if buf == nil {
		return
	}

	// Don't pool buffers that grew beyond xlarge size
	capacity := buf.Cap()
	if capacity > bufXLargeSize*2 {
		return
	}

	switch {
	case capacity <= bufSmallSize:
		bp.small.Put(buf)
	case capacity <= bufMediumSize:
		bp.medium.Put(buf)
	case capacity <= bufLargeSize:
		bp.large.Put(buf)
	default:
		bp.xlarge.Put(buf)
	}
}

func GetBuffer(hint int) *bytes.Buffer {
	return globalBufferPool.Get(hint)
}

func PutBuffer(buf *bytes.Buffer) {
	globalBufferPool.Put(buf)
}

// ByteSlice provides zero-copy string to byte conversion.
// SAFETY: The returned slice must not be modified.
func ByteSlice(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// String provides zero-copy byte to string conversion.
// SAFETY: The byte slice must not be modified after conversion.
func String(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(&b[0], len(b))
}

func AppendString(b []byte, s string) []byte {
	return append(b, ByteSlice(s)...)
}

type BytesBuffer struct {
	buf *bytes.Buffer
}

func NewBytesBuffer(buf *bytes.Buffer) *BytesBuffer {
	return &BytesBuffer{buf: buf}
}

func (b *BytesBuffer) WriteString(s string) {
	b.buf.Write(ByteSlice(s))
}

func (b *BytesBuffer) WriteByte(c byte) error {
	return b.buf.WriteByte(c)
}

func (b *BytesBuffer) WriteInt(i int64) {
	var tmp [20]byte
	written := formatInt(tmp[:], i)
	b.buf.Write(tmp[:written])
}

func (b *BytesBuffer) Write(p []byte) (int, error) {
	return b.buf.Write(p)
}

func (b *BytesBuffer) Bytes() []byte {
	return b.buf.Bytes()
}

// AppendTime writes a formatted timestamp directly into the buffer,
// avoiding the intermediate string allocation from time.Format.
func (b *BytesBuffer) AppendTime(t time.Time, layout string) {
	b.buf.Write(t.AppendFormat(b.buf.AvailableBuffer(), layout))
}

func (b *BytesBuffer) String() string {
	return String(b.buf.Bytes())
}

type Formatter struct {
	buf *bytes.Buffer
}

func NewFormatter(hint int) *Formatter {
	return &Formatter{
		buf: GetBuffer(hint),
	}
}

func (f *Formatter) WriteString(s string) *Formatter {
	f.buf.Write(ByteSlice(s))
	return f
}

func (f *Formatter) AppendByte(b byte) *Formatter {
	_ = f.buf.WriteByte(b)
	return f
}

func (f *Formatter) WriteInt(i int64) *Formatter {
	var tmp [20]byte
	written := formatInt(tmp[:], i)
	f.buf.Write(tmp[:written])
	return f
}

func (f *Formatter) Bytes() []byte {
	return f.buf.Bytes()
}

func (f *Formatter) String() string {
	return String(f.buf.Bytes())
}

func (f *Formatter) Release() {
	PutBuffer(f.buf)
	f.buf = nil
}

func formatInt(b []byte, i int64) int {
	if i == 0 {
		if len(b) > 0 {
			b[0] = '0'
			return 1
		}
		return 0
	}

	neg := i < 0
	// Convert to uint64 before negating to handle MinInt64, where -i overflows in int64.
	var u uint64
	if neg {
		u = uint64(-(i + 1)) + 1 //nolint:gosec // G115: two-step abs to avoid MinInt64 overflow, not a value conversion
	} else {
		u = uint64(i) //nolint:gosec // G115: i is non-negative here, conversion is safe
	}

	idx := len(b)
	for u > 0 && idx > 0 {
		idx--
		b[idx] = byte(u%10) + '0'
		u /= 10
	}

	if neg && idx > 0 {
		idx--
		b[idx] = '-'
	}

	written := len(b) - idx
	if idx > 0 {
		copy(b, b[idx:])
	}

	return written
}
