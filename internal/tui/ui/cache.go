package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"oto/internal/domain"
	"oto/internal/storage"
)

const manualLyricsOverridePrefix = "lyrics_manual_override:"

func manualLyricsOverrideKey(id string) string {
	return manualLyricsOverridePrefix + id
}

func mergeManualLyrics(base, override domain.LyricsResp) (domain.LyricsResp, bool) {
	if len(override.AllTracks) == 0 && (len(override.Synced) > 0 || override.Plain != "") {
		override.AllTracks = []domain.LyricsTrack{{
			Language: override.Language, Source: "lrclib", Synced: override.Synced, Plain: override.Plain,
			TrackName: override.Title, ArtistName: override.Artist,
		}}
	}
	if len(override.AllTracks) == 0 {
		return base, false
	}
	tracks := make([]domain.LyricsTrack, 0, len(override.AllTracks)+len(base.AllTracks))
	tracks = append(tracks, override.AllTracks...)
	for _, track := range base.AllTracks {
		if track.Source != "lrclib" && track.Language != "lrclib" {
			tracks = append(tracks, track)
		}
	}
	base.AllTracks = tracks
	base.SelectLanguage(override.LanguageIndex())
	return base, true
}

func getCachedLyrics(p domain.MusicProvider, store *storage.DB, id, title, artist string, duration int) (domain.LyricsResp, error) {
	if store == nil {
		return p.Lyrics(context.Background(), id, title, artist, duration)
	}
	var manual domain.LyricsResp
	manualFound := false
	if id != "" {
		if saved := store.GetSetting(manualLyricsOverrideKey(id)); saved != "" {
			manualFound = json.Unmarshal([]byte(saved), &manual) == nil &&
				(len(manual.AllTracks) > 0 || len(manual.Synced) > 0 || manual.Plain != "")
		}
	}
	cacheKey := fmt.Sprintf("%s|%s|%s", id, title, artist)

	cachedJSON, err := store.GetLyricsCache(cacheKey)
	var lr domain.LyricsResp
	var lyricsErr error
	cacheHit := false
	if err == nil && cachedJSON != "" {
		if json.Unmarshal([]byte(cachedJSON), &lr) == nil &&
			(len(lr.AllTracks) > 0 || len(lr.Synced) > 0 || lr.Plain != "") {
			log.Printf("[lyrics] local cache hit id=%s title=%q tracks=%d", id, title, len(lr.AllTracks))
			cacheHit = true
		} else {
			lr, lyricsErr = p.Lyrics(context.Background(), id, title, artist, duration)
		}
	} else {
		lr, lyricsErr = p.Lyrics(context.Background(), id, title, artist, duration)
	}

	if !cacheHit && lyricsErr == nil && (len(lr.AllTracks) > 0 || len(lr.Synced) > 0 || lr.Plain != "") {
		if lrJSON, jsonErr := json.Marshal(lr); jsonErr == nil {
			_ = store.PutLyricsCache(cacheKey, string(lrJSON), false)
		}
	}
	if manualFound {
		if merged, ok := mergeManualLyrics(lr, manual); ok {
			log.Printf("[lyrics] manual override merged id=%s title=%q", id, manual.Title)
			return merged, nil
		}
	}

	return lr, lyricsErr
}
