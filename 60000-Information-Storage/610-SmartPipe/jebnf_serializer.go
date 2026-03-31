package main

import (
	"bytes"
	"fmt"
	"strings"
)

// BranchTree represents the file-to-CAS mapping for a specific branch.
type BranchTree struct {
	Branch     string
	HeadSHA    string
	CapturedAt string
	Entries    []BranchEntry
	Dirs       []DirStat
}

type BranchEntry struct {
	Path       string
	Mode       string
	SHA        string // Logical content identity (invariant to packing)
	CasKey     string // Direct blob physical storage key (if stored individually)
	PackKey    string // Packfile physical storage key (if packed together)
	PackOffset int64  // Byte offset within the packfile
	Size       int64
}

type DirStat struct {
	Path       string
	EntryCount int
	TotalBytes int64
}

// ComponentRef represents a reference to a repo-level component.
type ComponentRef struct {
	Component   string
	CasKey      string
	Format      string
	RecordCount int
	CapturedAt  string
}

// Topology represents the full commit DAG across all branches.
type Topology struct {
	Repo       string
	CapturedAt string
	Branches   map[string]string
	Tags       map[string]Tag
	Commits    []Commit
	Trees      []Tree
}

type Tag struct {
	SHA     string
	Type    string
	Tagger  string
	Message string
	Date    string
}

type Commit struct {
	SHA       string
	Tree      string
	Parents   []string
	Author    Signature
	Committer Signature
	Message   string
}

type Signature struct {
	Name  string
	Email string
	Date  string
}

type Tree struct {
	SHA     string
	Entries []TreeEntry
}

type TreeEntry struct {
	Path string
	Mode string
	Type string
	SHA  string
}

// SerializeBranchTree writes a BranchTree struct into a strictly-formatted jeBNF string.
func SerializeBranchTree(t BranchTree) []byte {
	var b bytes.Buffer

	b.WriteString("::Olympus::Firehorse::BranchTree::v1 {\n")
	b.WriteString(fmt.Sprintf("\tBranch = %q;\n", t.Branch))
	b.WriteString(fmt.Sprintf("\tHeadSHA = %q;\n", t.HeadSHA))
	b.WriteString(fmt.Sprintf("\tCapturedAt = %q;\n\n", t.CapturedAt))

	b.WriteString("\tEntries [\n")
	for i, e := range t.Entries {
		if e.PackKey != "" {
			b.WriteString(fmt.Sprintf("\t\t{ Path = %q; Mode = %q; SHA = %q; PackKey = %q; PackOffset = %d; Size = %d; }",
				e.Path, e.Mode, e.SHA, e.PackKey, e.PackOffset, e.Size))
		} else {
			b.WriteString(fmt.Sprintf("\t\t{ Path = %q; Mode = %q; SHA = %q; CasKey = %q; Size = %d; }",
				e.Path, e.Mode, e.SHA, e.CasKey, e.Size))
		}
		if i < len(t.Entries)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("\t];\n\n")

	b.WriteString("\tDirectories [\n")
	for i, d := range t.Dirs {
		b.WriteString(fmt.Sprintf("\t\t{ Path = %q; EntryCount = %d; TotalBytes = %d; }",
			d.Path, d.EntryCount, d.TotalBytes))
		if i < len(t.Dirs)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("\t];\n")
	b.WriteString("}\n")

	return b.Bytes()
}

// SerializeComponentRef writes a ComponentRef struct into a strictly-formatted jeBNF string.
func SerializeComponentRef(c ComponentRef) []byte {
	var b bytes.Buffer

	b.WriteString("::Olympus::Firehorse::ComponentRef::v1 {\n")
	b.WriteString(fmt.Sprintf("\tComponent = %q;\n", c.Component))
	b.WriteString(fmt.Sprintf("\tCasKey = %q;\n", c.CasKey))
	b.WriteString(fmt.Sprintf("\tFormat = %q;\n", c.Format))
	b.WriteString(fmt.Sprintf("\tRecordCount = %d;\n", c.RecordCount))
	b.WriteString(fmt.Sprintf("\tCapturedAt = %q;\n", c.CapturedAt))
	b.WriteString("}\n")

	return b.Bytes()
}

// SerializeTopology writes a Topology struct into a strictly-formatted jeBNF string.
func SerializeTopology(t Topology) []byte {
	var b bytes.Buffer

	b.WriteString("::Olympus::Firehorse::Topology::v1 {\n")
	b.WriteString(fmt.Sprintf("\tRepo = %q;\n", t.Repo))
	b.WriteString(fmt.Sprintf("\tCapturedAt = %q;\n\n", t.CapturedAt))

	// Branches
	b.WriteString("\tBranches {\n")
	for name, sha := range t.Branches {
		b.WriteString(fmt.Sprintf("\t\t%q = %q;\n", name, sha))
	}
	b.WriteString("\t}\n\n")

	// Tags
	b.WriteString("\tTags {\n")
	for name, tag := range t.Tags {
		b.WriteString(fmt.Sprintf("\t\t%q { SHA = %q; Type = %q;", name, tag.SHA, tag.Type))
		if tag.Type == "annotated" {
			b.WriteString(fmt.Sprintf(" Tagger = %q; Message = %q; Date = %q;", tag.Tagger, tag.Message, tag.Date))
		}
		b.WriteString(" }\n")
	}
	b.WriteString("\t}\n\n")

	// Commits
	b.WriteString("\tCommits [\n")
	for i, c := range t.Commits {
		b.WriteString(fmt.Sprintf("\t\tCommit %q {\n", c.SHA))
		b.WriteString(fmt.Sprintf("\t\t\tTree = %q;\n", c.Tree))

		parents := make([]string, len(c.Parents))
		for pi, p := range c.Parents {
			parents[pi] = fmt.Sprintf("%q", p)
		}
		b.WriteString(fmt.Sprintf("\t\t\tParents = [%s];\n", strings.Join(parents, ", ")))

		b.WriteString(fmt.Sprintf("\t\t\tAuthor { Name = %q; Email = %q; Date = %q; }\n", c.Author.Name, c.Author.Email, c.Author.Date))
		b.WriteString(fmt.Sprintf("\t\t\tCommitter { Name = %q; Email = %q; Date = %q; }\n", c.Committer.Name, c.Committer.Email, c.Committer.Date))
		b.WriteString(fmt.Sprintf("\t\t\tMessage = %q;\n", sanitizeString(c.Message)))
		b.WriteString("\t\t}")
		if i < len(t.Commits)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("\t];\n\n")

	// Trees
	b.WriteString("\tTrees [\n")
	for i, tree := range t.Trees {
		b.WriteString(fmt.Sprintf("\t\tTree %q [\n", tree.SHA))
		for j, entry := range tree.Entries {
			b.WriteString(fmt.Sprintf("\t\t\t{ Path = %q; Mode = %q; Type = %q; SHA = %q; }",
				entry.Path, entry.Mode, entry.Type, entry.SHA))
			if j < len(tree.Entries)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString("\t\t]")
		if i < len(t.Trees)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("\t];\n")
	b.WriteString("}\n")

	return b.Bytes()
}

// sanitizeString removes unprintable characters or line breaks that might break basic jeBNF string parsing.
func sanitizeString(s string) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}
