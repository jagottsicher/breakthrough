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
			// TeamViewer's own log format: slashes in the date
			// ("2023/12/31 22:33:22.756"), two numeric PID/TID fields
			// and its own "S"/"S!!" severity marker (not a recognized
			// level) before the message. A real, user-reported gap —
			// same "/" vs "-" shape confirmed only loses the timestamp
			// for the sole reason that the separator differs.
			name: "generic timestamp with slash date, no recognized level",
			want: FormatGeneric,
			in: []string{
				"2023/12/31 22:33:22.756 11924 11924 S!! DBus: unable to unregister Object Path",
				"2023/12/31 22:33:22.755 11924 11924 S   NetworkControl shutdown done",
			},
		},
		{
			// Apache/CUPS/nginx's own Common/Combined Log Format access
			// log — confirmed against the user's own real CUPS
			// access_log, and the exact format nginx/apache2's own
			// default access logs already use too (both explicitly
			// asked about).
			name: "CLF access log (Apache/nginx/CUPS)",
			want: FormatCLF,
			in: []string{
				`localhost - - [03/Oct/2026:14:15:46 +0200] "POST / HTTP/1.1" 200 183 Renew-Subscription successful-ok`,
				`127.0.0.1 - - [10/Oct/2026:13:55:36 +0200] "GET /index.html HTTP/1.1" 200 2326 "-" "Mozilla/5.0"`,
			},
		},
		{
			// PHP-FPM's own default log shape — a real gap, same as
			// dpkg.log's: the bracketed "DD-Mon-YYYY HH:MM:SS" timestamp
			// matched no format at all before.
			name: "generic with PHP-FPM bracketed timestamp",
			want: FormatGeneric,
			in: []string{
				"[10-Oct-2026 13:55:36] WARNING: [pool www] child 1234 said into stderr: a warning",
				"[10-Oct-2026 13:55:37] NOTICE: fpm is running, pid 1234",
			},
		},
		{
			// Apache's own error log default format (httpd's
			// ErrorLogFormat %{u}t) — a real gap, same reasoning: the
			// bracketed "Day Mon DD HH:MM:SS.ffffff YYYY" timestamp
			// matched no format at all before, nor did "[core:error]"'s
			// own module-prefixed level.
			name: "generic with Apache error log timestamp",
			want: FormatGeneric,
			in: []string{
				"[Thu Oct 09 13:55:36.123456 2026] [core:error] [pid 1234:tid 5678] AH00646: something failed",
				"[Thu Oct 09 13:55:37.654321 2026] [core:warn] a warning",
			},
		},
		{
			// Nginx's own error log default format — needs no new
			// timestamp alternative at all (its slash date already
			// matches the plain ISO-ish one), only the broadened level
			// vocabulary ("notice"/"crit" weren't recognized before).
			name: "generic nginx error log",
			want: FormatGeneric,
			in: []string{
				`2026/10/09 13:55:36 [error] 1234#0: *5 connect() failed`,
				`2026/10/09 13:55:37 [notice] 1234#0: signal process started`,
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
