// Package panel renders wattop's terminal widgets -- the SoC strip (soc.go),
// horizontal gauges (gauge.go) and the braille sparkline helper (spark.go)
// -- from domain types and a resolved theme.Roles. No widget ever reads a
// theme.Palette field directly.
package panel
