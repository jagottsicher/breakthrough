package logview

import "testing"

func TestDetect(t *testing.T) {
	tests := []struct {
		name string
		want Format
		in   []string
	}{
		{
			name: "json lines",
			want: FormatJSON,
			in: []string{
				`{"time":"2026-10-07T16:04:21Z","level":"error","msg":"timeout"}`,
				`{"time":"2026-10-07T16:04:22Z","level":"info","msg":"reconnecting"}`,
			},
		},
		{
			name: "rfc3164 syslog",
			want: FormatSyslogRFC3164,
			in: []string{
				"Oct  7 16:04:22 server kernel: eth0: link down",
				"Oct  7 16:04:23 server app[123]: started",
			},
		},
		{
			// Modern rsyslog's own default "high precision" file
			// format (RSYSLOG_FileFormat) — an ISO8601 timestamp
			// instead of RFC 3164's classic "Mon _2 HH:MM:SS", the
			// rest (hostname, tag[pid]:, message) identical. A real
			// gap confirmed against the user's own real /var/log/
			// syslog and /var/log/kern.log on more than one machine.
			name: "rfc3164 syslog with ISO8601 timestamp",
			want: FormatSyslogRFC3164,
			in: []string{
				"2026-10-09T10:48:44.551263+02:00 kalimashaktide sudo: pam_ecryptfs: pam_sm_authenticate: /home/jens is already mounted",
				"2026-10-09T10:45:01.481818+02:00 kalimashaktide CRON[2961797]: (root) CMD (command -v debian-sa1 > /dev/null && debian-sa1 1 1)",
			},
		},
		{
			name: "rfc5424 syslog",
			want: FormatSyslogRFC5424,
			in: []string{
				"<34>1 2026-10-07T16:04:22Z server app 123 ID1 - timeout",
			},
		},
		{
			name: "generic timestamp+level",
			want: FormatGeneric,
			in: []string{
				"2026-10-07 16:04:23 ERROR database connection timeout",
				"2026-10-07 16:04:24 INFO  connection established",
			},
		},
		{
			// Python's logging.Formatter default datefmt — a comma, not
			// a dot, before the milliseconds. A real gap this pattern
			// used to miss entirely (see reGenericTAB's own doc
			// comment), falling back to FormatPlain for the whole file.
			name: "generic timestamp+level with comma milliseconds",
			want: FormatGeneric,
			in: []string{
				"2026-10-07 16:04:23,123 ERROR database connection timeout",
				"2026-10-07 16:04:24,456 INFO connection established",
			},
		},
		{
			// dpkg.log's own real shape — a perfectly good, parseable
			// timestamp, but no level at all (status/install/trigproc
			// are dpkg's own action words, none of them a recognized
			// level). Used to fall all the way back to FormatPlain for
			// the sole reason that no level followed — a real,
			// user-reported gap ("alles als message zu behandeln, wenn
			// man die Zeit nicht lesen kann, ist sehr dürftig").
			name: "generic timestamp with no level at all",
			want: FormatGeneric,
			in: []string{
				"2026-09-30 20:05:33 status installed man-db:amd64 2.13.1-1",
				"2026-09-30 20:05:33 trigproc man-db:amd64 2.13.1-1 <none>",
			},
		},
		{
			name: "plain fallback",
			want: FormatPlain,
			in: []string{
				"just some unstructured text",
				"another line with no shape at all",
			},
		},
		{
			name: "empty sample",
			want: FormatPlain,
			in:   nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Detect(tc.in); got != tc.want {
				t.Errorf("Detect(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestDetectGenericWithISO8601TimestampIsNotMisdetectedAsRFC3164 pins
// that reRFC3164's own new ISO8601 alternative (see its own doc
// comment) stays correctly separated from a plain "TIMESTAMP LEVEL
// message" line sharing the exact same timestamp shape: the extra
// HOSTNAME + TAG[PID]: structure reRFC3164 now requires for that
// alternative is what a generic level+message line doesn't have.
func TestDetectGenericWithISO8601TimestampIsNotMisdetectedAsRFC3164(t *testing.T) {
	sample := []string{
		"2026-10-07T16:04:23Z ERROR database connection timeout",
		"2026-10-07T16:04:24Z INFO connection established",
	}
	if got := Detect(sample); got != FormatGeneric {
		t.Errorf("Detect = %v, want FormatGeneric — an ISO8601 timestamp followed by LEVEL message must not be mistaken for syslog's hostname+tag: shape", got)
	}
}

func TestDetectBelowThresholdFallsBackToPlain(t *testing.T) {
	sample := []string{
		`{"time":"2026-10-07T16:04:21Z","level":"error","msg":"timeout"}`,
		"not json at all",
		"neither is this",
		"nor this one",
	}
	if got := Detect(sample); got != FormatPlain {
		t.Errorf("Detect = %v, want FormatPlain (only 1/4 lines matched JSON)", got)
	}
}
