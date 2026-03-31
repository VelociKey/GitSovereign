package bridge

import (
	"fmt"
	"io"
	"olympus.fleet/00SDLC/OlympusLogicLibrary/60000-Information-Storage/90200-Logic-Libraries/110-gitsov-key"
	"olympus.fleet/00SDLC/OlympusLogicLibrary/60000-Information-Storage/90200-Logic-Libraries/120-adph"
)

type DedupeStreamer struct {
	Index  *adph.Table[gitsovkey.GitSovKey, string]
	Egress io.WriteCloser
}

func (d *DedupeStreamer) HandleObject(key gitsovkey.GitSovKey, source io.Reader) error {
	if _, found := d.Index.Lookup(key); found {
		return nil 
	}
	_, err := io.Copy(d.Egress, source)
	if err != nil { return err }
	d.Index.Add(key, fmt.Sprintf("gs://gdrive-sovereign-vault/%s", key.Hex()))
	return nil
}

type SwallowEgress struct{}
func NewSwallowEgress() *SwallowEgress { return &SwallowEgress{} }
func (s *SwallowEgress) Write(p []byte) (int, error) { return len(p), nil }
func (s *SwallowEgress) Close() error { return nil }
