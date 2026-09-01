package internal

import (
	indexerv1 "github.com/Muxcore-Media/contracts-indexer/muxcore/indexer/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const defaultTorznabIndexerID = int32(1)

func mapHits(hits []torznabHit, fallbackIndexerName string) []*indexerv1.SearchResult {
	out := make([]*indexerv1.SearchResult, 0, len(hits))
	for _, h := range hits {
		indexerName := h.IndexerName
		if indexerName == "" {
			indexerName = fallbackIndexerName
		}
		indexerID := h.IndexerID
		if indexerID <= 0 {
			indexerID = defaultTorznabIndexerID
		}
		r := &indexerv1.SearchResult{
			Guid:             h.GUID,
			Title:            h.Title,
			InfoUrl:          stripSensitiveQueryParams(h.InfoURL),
			DownloadUrl:      stripSensitiveQueryParams(h.DownloadURL),
			Size:             h.Size,
			Seeders:          h.Seeders,
			Peers:            h.Peers,
			IndexerName:      indexerName,
			IndexerId:        indexerID,
			Category:         h.Category,
			ImdbId:           h.IMDB,
			TmdbId:           h.TMDB,
			TvdbId:           h.TVDB,
			DownloadProtocol: normalizeIndexerProtocol(h.Protocol, h.DownloadURL),
		}
		if !h.PubDate.IsZero() {
			r.PublishDate = timestamppb.New(h.PubDate)
		}
		out = append(out, r)
	}
	return out
}
