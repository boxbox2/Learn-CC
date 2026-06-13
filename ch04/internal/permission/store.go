package permission

import (
	"bytes"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type YAMLRuleStore struct {
	ProjectDir string
}

func (s YAMLRuleStore) AppendAllowRule(pattern string) error {
	path := filepath.Join(s.ProjectDir, "mewcode.local.yaml")
	var root yaml.Node
	if data, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(data, &root); err != nil {
			return err
		}
	} else if os.IsNotExist(err) {
		root = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	} else {
		return err
	}
	doc := ensureDocumentMapping(&root)
	permissions := ensureMapChild(doc, "permissions")
	rules := ensureSeqChild(permissions, "rules")
	rules.Content = append(rules.Content, ruleNode(ActionAllow, pattern))
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		_ = enc.Close()
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func ensureDocumentMapping(root *yaml.Node) *yaml.Node {
	if root.Kind != yaml.DocumentNode {
		*root = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		root.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
	}
	return root.Content[0]
}

func ensureMapChild(parent *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			if parent.Content[i+1].Kind != yaml.MappingNode {
				parent.Content[i+1] = &yaml.Node{Kind: yaml.MappingNode}
			}
			return parent.Content[i+1]
		}
	}
	k := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
	v := &yaml.Node{Kind: yaml.MappingNode}
	parent.Content = append(parent.Content, k, v)
	return v
}

func ensureSeqChild(parent *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			if parent.Content[i+1].Kind != yaml.SequenceNode {
				parent.Content[i+1] = &yaml.Node{Kind: yaml.SequenceNode}
			}
			return parent.Content[i+1]
		}
	}
	k := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
	v := &yaml.Node{Kind: yaml.SequenceNode}
	parent.Content = append(parent.Content, k, v)
	return v
}

func ruleNode(action Action, pattern string) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "action"},
		{Kind: yaml.ScalarNode, Value: string(action)},
		{Kind: yaml.ScalarNode, Value: "pattern"},
		{Kind: yaml.ScalarNode, Value: pattern},
	}}
}
