// Package log configures file-only structured logging for dbt.
//
// Logging is opt-in via --debug and writes JSON records to
// $XDG_STATE_HOME/dbt/dbt.log. Connection strings are redacted before they
// reach the log.
package log
