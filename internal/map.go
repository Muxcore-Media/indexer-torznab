package internal

import (
	indexerv1 "github.com/Muxcore-Media/contracts-indexer/muxcore/indexer/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func mapHits(hits []torznabHit, indexerName string) []*indexerv1.SearchResult {
	out := make([]*indexerv1.SearchResult, 0, len(hits))
	for _, h := range hits {
		r := &indexerv1.SearchResult{
			Guid:             h.GUID,
			Title:            h.Title,
			InfoUrl:          h.InfoURL,
			DownloadUrl:      h.DownloadURL,
			Size:             h.Size,
			Seeders:          h.Seeders,
			Peers:            h.Peers,
			IndexerName:      indexerName,
			IndexerId:        indexerID,
			Category:         h.Category,
			ImdbId:           h.IMDB,
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
