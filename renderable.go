package velocity

import "io"

// Renderable is implemented by any value that can write a formatted representation
// of itself to an io.Writer.
//
// The primary use is Logger.Render and Logger.RenderRaw, which route Renderable
// values through the console writer with appropriate indentation.
// JSON writers silently ignore Render calls since they write structured data.
type Renderable interface {
	Render(w io.Writer) error
}
