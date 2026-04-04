package velocity

type Colour struct {
	colour256 int
	r, g, b   uint8
	isRGB     bool
}

func RGB(r, g, b uint8) Colour {
	return Colour{
		r:     r,
		g:     g,
		b:     b,
		isRGB: true,
	}
}

func Colour256(c int) Colour {
	if c < 0 || c > 255 {
		c = 0
	}
	return Colour{colour256: c}
}

// ANSI generates the escape sequence for foreground or background colours.
func (c Colour) ANSI(foreground bool) string {
	if c.isRGB {
		if foreground {
			return "\033[38;2;" + itoa(int(c.r)) + ";" + itoa(int(c.g)) + ";" + itoa(int(c.b)) + "m"
		}
		return "\033[48;2;" + itoa(int(c.r)) + ";" + itoa(int(c.g)) + ";" + itoa(int(c.b)) + "m"
	}
	if foreground {
		return "\033[38;5;" + itoa(c.colour256) + "m"
	}
	return "\033[48;5;" + itoa(c.colour256) + "m"
}

const Reset = "\033[0m"

type Theme struct {
	// Per-level foreground codes, indexed by Level constant.
	cachedLevelFg [6]string
	Name          string

	// Pre-computed ANSI escape sequences for hot-path rendering.
	cachedTimestampFg string
	cachedMessageFg   string
	cachedFieldKeyFg  string
	cachedFieldValFg  string
	cachedErrorValFg  string

	DebugColour Colour
	InfoColour  Colour
	WarnColour  Colour
	ErrorColour Colour
	FatalColour Colour

	TimestampColour Colour
	MessageColour   Colour
	FieldKeyColour  Colour
	FieldValColour  Colour
	ErrorValColour  Colour

	// Status indicator colours for operation results
	StatusOKColour   Colour
	StatusFailColour Colour
	StatusWarnColour Colour
	StatusInfoColour Colour

	// Table header colour
	TableHeader Colour
}

// Cache pre-computes ANSI sequences for all colours used in the hot-path template renderer.
// Call this after constructing a custom Theme to avoid per-log allocations.
func (t *Theme) Cache() {
	t.cachedTimestampFg = t.TimestampColour.ANSI(true)
	t.cachedMessageFg = t.MessageColour.ANSI(true)
	t.cachedFieldKeyFg = t.FieldKeyColour.ANSI(true)
	t.cachedFieldValFg = t.FieldValColour.ANSI(true)
	t.cachedErrorValFg = t.ErrorValColour.ANSI(true)
	t.cachedLevelFg[LevelDebug] = t.DebugColour.ANSI(true)
	t.cachedLevelFg[LevelInfo] = t.InfoColour.ANSI(true)
	t.cachedLevelFg[LevelWarn] = t.WarnColour.ANSI(true)
	t.cachedLevelFg[LevelError] = t.ErrorColour.ANSI(true)
	t.cachedLevelFg[LevelFatal] = t.FatalColour.ANSI(true)
	t.cachedLevelFg[LevelOff] = t.InfoColour.ANSI(true)
}

// cachedTheme calls Cache on t and returns it, used for package-level theme initialisation.
func cachedTheme(t Theme) *Theme {
	t.Cache()
	return &t
}

var ThemeNightOwl = cachedTheme(Theme{
	Name:             "Night Owl",
	DebugColour:      RGB(0xC7, 0x92, 0xEA), // #C792EA
	InfoColour:       RGB(0x82, 0xAA, 0xFF), // #82AAFF
	WarnColour:       RGB(0xFF, 0xCB, 0x6B), // #FFCB6B
	ErrorColour:      RGB(0xFF, 0x55, 0x72), // #FF5572
	FatalColour:      RGB(0xFF, 0x00, 0x00),
	TimestampColour:  RGB(0x7E, 0x8E, 0xA6),
	MessageColour:    RGB(0xE0, 0xE0, 0xE0),
	FieldKeyColour:   RGB(0x7E, 0x8E, 0xA6),
	FieldValColour:   RGB(0xD3, 0xD3, 0xD3),
	ErrorValColour:   RGB(0xFF, 0x55, 0x72),
	StatusOKColour:   RGB(0x80, 0xD4, 0xAA), // Green #80D4AA
	StatusFailColour: RGB(0xFF, 0x55, 0x72), // Red #FF5572
	StatusWarnColour: RGB(0xFF, 0xCB, 0x6B), // Yellow #FFCB6B
	StatusInfoColour: RGB(0x82, 0xAA, 0xFF), // Blue #82AAFF
	TableHeader:      RGB(0x7F, 0xD3, 0xFF), // Teal #7FD3FF
})

var ThemeSolarized = cachedTheme(Theme{
	Name:             "Solarized",
	DebugColour:      Colour256(61),
	InfoColour:       Colour256(33),
	WarnColour:       Colour256(136),
	ErrorColour:      Colour256(160),
	FatalColour:      Colour256(124),
	TimestampColour:  Colour256(8),
	MessageColour:    Colour256(7),
	FieldKeyColour:   Colour256(8),
	FieldValColour:   Colour256(7),
	ErrorValColour:   Colour256(160),
	StatusOKColour:   Colour256(64),  // Green
	StatusFailColour: Colour256(160), // Red
	StatusWarnColour: Colour256(136), // Yellow
	StatusInfoColour: Colour256(33),  // Blue
	TableHeader:      Colour256(37),  // Cyan
})

var ThemeDracula = cachedTheme(Theme{
	Name:             "Dracula",
	DebugColour:      Colour256(141),
	InfoColour:       Colour256(81),
	WarnColour:       Colour256(228),
	ErrorColour:      Colour256(212),
	FatalColour:      Colour256(196),
	TimestampColour:  Colour256(59),
	MessageColour:    Colour256(231),
	FieldKeyColour:   Colour256(59),
	FieldValColour:   Colour256(188),
	ErrorValColour:   Colour256(212),
	StatusOKColour:   Colour256(84),  // Green
	StatusFailColour: Colour256(212), // Red
	StatusWarnColour: Colour256(228), // Yellow
	StatusInfoColour: Colour256(81),  // Blue
	TableHeader:      Colour256(87),  // Cyan
})

var ThemeNord = cachedTheme(Theme{
	Name:             "Nord",
	DebugColour:      Colour256(139),
	InfoColour:       Colour256(109),
	WarnColour:       Colour256(180),
	ErrorColour:      Colour256(191),
	FatalColour:      Colour256(167),
	TimestampColour:  Colour256(59),
	MessageColour:    Colour256(216),
	FieldKeyColour:   Colour256(59),
	FieldValColour:   Colour256(188),
	ErrorValColour:   Colour256(191),
	StatusOKColour:   Colour256(108), // Green
	StatusFailColour: Colour256(167), // Red
	StatusWarnColour: Colour256(180), // Yellow
	StatusInfoColour: Colour256(109), // Blue
	TableHeader:      Colour256(110), // Frost Blue
})

func (t *Theme) GetColourForLevel(level Level) Colour {
	switch level {
	case LevelDebug:
		return t.DebugColour
	case LevelInfo:
		return t.InfoColour
	case LevelWarn:
		return t.WarnColour
	case LevelError:
		return t.ErrorColour
	case LevelFatal:
		return t.FatalColour
	case LevelOff:
		return t.InfoColour
	}
	return t.InfoColour
}

// StatusFormatter provides colour-aware status indicator formatting for operation results.
// Pre-caches ANSI codes at initialization for zero-allocation formatting.
type StatusFormatter struct {
	okCode    string
	failCode  string
	warnCode  string
	infoCode  string
	resetCode string // Reset to message colour instead of terminal default
	enabled   bool
}

// NewStatusFormatter creates a formatter that respects terminal capabilities and theme.
// Pass nil theme to disable colours.
func NewStatusFormatter(theme *Theme, isTTY bool) *StatusFormatter {
	sf := &StatusFormatter{
		enabled: isTTY && theme != nil,
	}

	if sf.enabled {
		sf.okCode = theme.StatusOKColour.ANSI(true)
		sf.failCode = theme.StatusFailColour.ANSI(true)
		sf.warnCode = theme.StatusWarnColour.ANSI(true)
		sf.infoCode = theme.StatusInfoColour.ANSI(true)
		// Reset to message colour instead of terminal default to maintain log colour consistency
		sf.resetCode = theme.MessageColour.ANSI(true)
	}

	return sf
}

// Okay formats an OK status with green colour when enabled.
func (sf *StatusFormatter) Okay(text string) string {
	if !sf.enabled {
		return text
	}
	return sf.okCode + text + sf.resetCode
}

// Fail formats a FAIL status with red colour when enabled.
func (sf *StatusFormatter) Fail(text string) string {
	if !sf.enabled {
		return text
	}
	return sf.failCode + text + sf.resetCode
}

// Warn formats a WARN status with yellow colour when enabled.
func (sf *StatusFormatter) Warn(text string) string {
	if !sf.enabled {
		return text
	}
	return sf.warnCode + text + sf.resetCode
}

// Info formats an INFO status with blue colour when enabled.
func (sf *StatusFormatter) Info(text string) string {
	if !sf.enabled {
		return text
	}
	return sf.infoCode + text + sf.resetCode
}
