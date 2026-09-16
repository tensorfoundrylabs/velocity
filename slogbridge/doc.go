// Package slogbridge bridges log/slog to a velocity Logger.
//
// NewHandler wraps a velocity Logger as a slog.Handler; NewLogger returns a
// *slog.Logger bound to it. WithAttrs pre-converts attributes to velocity
// fields and WithGroup caches the dotted group prefix, so both are immutable.
// LogValuer values are resolved and record times are honoured.
//
// A record whose level maps to velocity's LevelFatal is delivered like any
// other record: it is exempt from sampling but never invokes the fatal
// handler or exits the process. Only Logger.Fatal has process-control
// semantics. Dispatch observes the logger's closure; records logged after
// Close are dropped without panic.
package slogbridge
