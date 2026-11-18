package deploy

// LegacyC81MigrationResult reports a verified successor manifest after the
// restricted same-APP migration has refenced and retired its child.
type LegacyC81MigrationResult struct {
	ManifestDigest string
	Recovered      bool
}
