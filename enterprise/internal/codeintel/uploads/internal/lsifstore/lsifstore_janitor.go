package lsifstore

import (
	"context"
	"time"

	"github.com/keegancsmith/sqlf"
	otlog "github.com/opentracing/opentracing-go/log"

	"github.com/sourcegraph/sourcegraph/internal/database/basestore"
	"github.com/sourcegraph/sourcegraph/internal/observation"
)

func (s *store) DeleteUnreferencedDocuments(ctx context.Context, batchSize int, maxAge time.Duration, now time.Time) (_, _ int, err error) {
	ctx, _, endObservation := s.operations.idsWithMeta.With(ctx, &err, observation.Args{LogFields: []otlog.Field{
		otlog.String("maxAge", maxAge.String()),
	}})
	defer endObservation(1, observation.Args{})

	rows, err := s.db.Query(ctx, sqlf.Sprintf(
		deleteUnreferencedDocumentsQuery,
		now,
		maxAge/time.Second,
		batchSize,
	))
	if err != nil {
		return 0, 0, err
	}
	defer func() { err = basestore.CloseRows(rows, err) }()

	var c1, c2 int
	for rows.Next() {
		if err := rows.Scan(&c1, &c2); err != nil {
			return 0, 0, err
		}
	}

	return c1, c2, nil
}

const deleteUnreferencedDocumentsQuery = `
WITH
candidates AS (
	SELECT id, document_id
	FROM codeintel_scip_documents_dereference_logs log
	WHERE %s - log.last_removal_time > (%s * interval '1 second')
	ORDER BY last_removal_time DESC, document_id
	LIMIT %s
	FOR UPDATE SKIP LOCKED
),
locked_documents AS (
	SELECT sd.id
	FROM candidates d
	JOIN codeintel_scip_documents sd ON sd.id = d.document_id
	WHERE NOT EXISTS (SELECT 1 FROM codeintel_scip_document_lookup sdl WHERE sdl.document_id = sd.id)
	ORDER BY sd.id
	FOR UPDATE OF sd
),
deleted_documents AS (
	DELETE FROM codeintel_scip_documents
	WHERE id IN (SELECT id FROM locked_documents)
	RETURNING id
),
deleted_candidates AS (
	DELETE FROM codeintel_scip_documents_dereference_logs
	WHERE id IN (SELECT id FROM candidates)
	RETURNING id
)
SELECT
	(SELECT COUNT(*) FROM candidates),
	(SELECT COUNT(*) FROM deleted_documents)
`
