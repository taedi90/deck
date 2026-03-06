package bundle

func InspectManifest(bundlePath string) ([]ManifestEntry, error) {
	return loadManifestEntries(bundlePath)
}
