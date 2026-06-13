package prompt

import (
	"sort"
	"strings"
)

type SectionKind string

const (
	SectionIdentity    SectionKind = "identity"
	SectionConstraints SectionKind = "system_constraints"
	SectionTaskModes   SectionKind = "task_modes"
	SectionActions     SectionKind = "actions"
	SectionTools       SectionKind = "tools"
	SectionTone        SectionKind = "tone"
	SectionOutput      SectionKind = "output"
	SectionEnvironment SectionKind = "environment"
	SectionCustom      SectionKind = "custom"
	SectionSkills      SectionKind = "skills"
	SectionMemory      SectionKind = "memory"
)

type Section struct {
	Kind    SectionKind
	Title   string
	Content string
}

type Builder struct {
	sections []Section
}

func NewBuilder() *Builder {
	return &Builder{}
}

func (b *Builder) Add(section Section) {
	if strings.TrimSpace(section.Content) == "" {
		return
	}
	b.sections = append(b.sections, section)
}

func (b *Builder) Build() string {
	sections := append([]Section(nil), b.sections...)
	sort.SliceStable(sections, func(i, j int) bool {
		return sectionPriority(sections[i].Kind) < sectionPriority(sections[j].Kind)
	})
	parts := make([]string, 0, len(sections))
	for _, section := range sections {
		title := strings.TrimSpace(section.Title)
		content := strings.TrimSpace(section.Content)
		if title == "" {
			title = string(section.Kind)
		}
		parts = append(parts, "## "+title+"\n"+content)
	}
	return strings.Join(parts, "\n\n")
}

func sectionPriority(kind SectionKind) int {
	switch kind {
	case SectionIdentity:
		return 10
	case SectionConstraints:
		return 20
	case SectionTaskModes:
		return 30
	case SectionActions:
		return 40
	case SectionTools:
		return 50
	case SectionTone:
		return 60
	case SectionOutput:
		return 70
	case SectionEnvironment:
		return 80
	case SectionCustom:
		return 90
	case SectionSkills:
		return 100
	case SectionMemory:
		return 110
	default:
		return 1000
	}
}
