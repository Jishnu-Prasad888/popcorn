package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"

	"github.com/Jishnu-Prasad888/popcorn/internal/probe"
)

var (
	reIntroChapter   = regexp.MustCompile(`(?i)^(intro|opening|op|opening credits|title sequence|recap)$|\b(intro|opening)\b`)
	reCreditsChapter = regexp.MustCompile(`(?i)^(credits|ending|ed|end credits|outro|closing credits)$|\b(credits|ending|outro)\b`)
)

// SaveProbe stores the result of probing a file. A probe error is recorded so
// the file is not probed again until it changes.
func SaveProbe(ctx context.Context, db *sql.DB, fileID string, size int64, mtime string, res *probe.Result, probeErr error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if probeErr != nil || res == nil {
		msg := "probe failed"
		if probeErr != nil {
			msg = probeErr.Error()
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO video_files (file_id, probe_size, probe_mtime, probed_at, probe_error)
			 VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT(file_id) DO UPDATE SET probe_size = excluded.probe_size,
			   probe_mtime = excluded.probe_mtime, probed_at = excluded.probed_at,
			   probe_error = excluded.probe_error`,
			fileID, size, mtime, now(), msg); err != nil {
			return err
		}
		return tx.Commit()
	}
	chapters, _ := json.Marshal(res.Chapters)
	var width, height int
	var vcodec, vprofile, pixfmt string
	var fps float64
	if v := res.Video(); v != nil {
		width, height, vcodec, vprofile, pixfmt, fps = v.Width, v.Height, v.Codec, v.Profile, v.PixFmt, v.FrameRate
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO video_files (file_id, container, duration_secs, bitrate, width, height, video_codec,
		                          video_profile, pix_fmt, frame_rate, hdr, chapters, probe_size, probe_mtime,
		                          probed_at, probe_error)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)
		 ON CONFLICT(file_id) DO UPDATE SET container = excluded.container,
		   duration_secs = excluded.duration_secs, bitrate = excluded.bitrate, width = excluded.width,
		   height = excluded.height, video_codec = excluded.video_codec,
		   video_profile = excluded.video_profile, pix_fmt = excluded.pix_fmt,
		   frame_rate = excluded.frame_rate, hdr = excluded.hdr, chapters = excluded.chapters,
		   probe_size = excluded.probe_size, probe_mtime = excluded.probe_mtime,
		   probed_at = excluded.probed_at, probe_error = NULL`,
		fileID, res.Container, res.Duration, res.Bitrate, width, height, vcodec, vprofile, pixfmt, fps,
		nullStr(res.HDR()), string(chapters), size, mtime, now()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM media_streams WHERE file_id = ?`, fileID); err != nil {
		return err
	}
	for _, s := range res.Streams {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO media_streams (file_id, stream_index, kind, type_index, codec, profile, language, title,
			                            channels, channel_layout, sample_rate, bitrate, width, height,
			                            is_default, is_forced, is_hearing_impaired)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			fileID, s.Index, s.Kind, s.TypeIndex, s.Codec, nullStr(s.Profile), nullStr(s.Language),
			nullStr(s.Title), s.Channels, nullStr(s.ChannelLayout), s.SampleRate, s.Bitrate, s.Width,
			s.Height, boolInt(s.IsDefault), boolInt(s.IsForced), boolInt(s.IsHearingImpaired)); err != nil {
			return err
		}
	}
	// Chapter markers named "Intro"/"Credits" give an episode its skip points,
	// unless a person already set them.
	introStart, introEnd, creditsStart := -1.0, -1.0, -1.0
	for _, c := range res.Chapters {
		switch {
		case introStart < 0 && reIntroChapter.MatchString(c.Title) && c.Start < res.Duration/3:
			introStart, introEnd = c.Start, c.End
		case creditsStart < 0 && reCreditsChapter.MatchString(c.Title) && c.Start > res.Duration/2:
			creditsStart = c.Start
		}
	}
	if introStart >= 0 && introEnd > introStart {
		if _, err := tx.ExecContext(ctx,
			`UPDATE episodes SET intro_start = ?, intro_end = ?
			 WHERE intro_start IS NULL AND id IN (SELECT episode_id FROM episode_files WHERE file_id = ?)`,
			introStart, introEnd, fileID); err != nil {
			return err
		}
	}
	if creditsStart > 0 {
		if _, err := tx.ExecContext(ctx,
			`UPDATE episodes SET credits_start = ?
			 WHERE credits_start IS NULL AND id IN (SELECT episode_id FROM episode_files WHERE file_id = ?)`,
			creditsStart, fileID); err != nil {
			return err
		}
	}
	// A movie or episode without a runtime takes it from its file.
	mins := int(res.Duration/60 + 0.5)
	if mins > 0 {
		if _, err := tx.ExecContext(ctx,
			`UPDATE movies SET runtime_mins = ? WHERE runtime_mins IS NULL
			 AND id IN (SELECT movie_id FROM movie_files WHERE file_id = ?)`, mins, fileID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE episodes SET runtime_mins = ? WHERE runtime_mins IS NULL
			 AND id IN (SELECT episode_id FROM episode_files WHERE file_id = ?)`, mins, fileID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
