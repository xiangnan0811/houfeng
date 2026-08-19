# Search, Records Center, and Global Search Design

## 1. Boundary

Search is a derived projection of authoritative current Records revisions and
collaboration facts. The Records Center reads that projection; record detail and
revision history continue to read Records Core.

## 2. Migration 0056

`0056_create_record_search.sql` enables approved PostgreSQL text extensions and
creates:

- `record_search_documents` with current revision, normalized plain text,
  vectors/trigrams, lifecycle, structured filter columns, auth digest, and hash;
- `record_search_subjects` for typed subject/ref filtering;
- `record_search_generations` and `record_search_rebuild_jobs`;
- indexes matching the reviewed filter/sort/query shapes.

The migration does not copy drafts, comments, attachment bytes, evidence
payloads, or activity rows. Its current APP ACL fragment grants only the
operations used by the runtime projector/query path.

## 3. Projection

`SearchRevisionParticipant` receives the committed complete revision plus
collaboration projection and writes one current document in the same
transaction. Markdown normalization uses the shared dialect parser and a
server-owned plain-text extractor.

Rebuild reads authoritative rows in a fixed generation, writes a shadow
generation, validates count/hash/authorization coverage, and atomically
publishes it. Concurrent commits either join the new generation through a
journal/watermark contract or force retry; no incomplete generation is queried.

Archive keeps the document with lifecycle filter; permanent delete removes it
under fence. Import and restore rebuild from authoritative rows.

## 4. Query and cursor

```go
type RecordSearchQuery struct {
	Text             string
	Types            []string
	Statuses         []string
	StatusGroups     []string
	Lifecycles       []string
	SubjectRefs      []SubjectRef
	OwnerIDs         []string
	ParticipantIDs   []string
	Followup          TimeStateFilter
	Action            ActionFilter
	Tags              []string
	Occurred          TimeRange
	Updated           TimeRange
	Sort              RecordSearchSort
	PageSize          int
	Cursor            string
}
```

Values within one repeated field are OR; distinct fields are AND. Action
matching uses `EXISTS` so one record returns once. Query normalization is shared
by cursor signing and Web URL encoding.

Cursor payload binds version, query digest, authorization namespace, published
generation, page size, expiry, and the full sort tuple. It is opaque to Web.

The store applies authorization before returning IDs, fields, snippets, counts,
or facets. Unauthorized and missing resources are indistinguishable.

## 5. HTTP and Web

Server endpoints provide records, drafts, filter metadata, and bounded global
search results. DTOs use response allowlists.

`/records` and `/records/drafts` are lazy routes with canonical URL state.
First-empty differs from query-no-results. Local source errors remain local and
do not render stale results as current. Global search displays a bounded Records
group with an explicit link to the full filtered route.

## 6. Security and operations

No query text or result content is logged. Metrics use bounded query class,
latency, count bucket, and reason code. Rate/size/page limits apply before
expensive work.

Search health exposes generation/lag/failure without record IDs or text.
Permanent delete readiness requires the search adapter. Backup excludes derived
documents and rebuilds after restore.

## 7. Compatibility and rollback

The current development database can be rebuilt; no prior migration ordering or
legacy content compatibility is supported. Disabling search routes/projector
leaves authoritative Records intact. `0056` is additive and no down migration is
run.
