package recordmarkdown

import (
	"regexp"
	"strings"
)

var (
	houfengRefCommentPattern = regexp.MustCompile(`^<!-- houfeng-ref:v1 (evidence|attachment) ([A-Za-z0-9][A-Za-z0-9_-]{0,127}) -->$`)
	houfengRefLinkPattern    = regexp.MustCompile(`^\[(.+)\]\(houfeng-(evidence|attachment):([A-Za-z0-9][A-Za-z0-9_-]{0,127})\)$`)
)

func authorizedReferenceSet(references []DocumentReference) map[string]struct{} {
	allowed := make(map[string]struct{}, len(references))
	for _, reference := range references {
		if reference.Kind != "evidence" && reference.Kind != "attachment" || reference.ID == "" {
			continue
		}
		allowed[reference.Kind+"\n"+reference.ID] = struct{}{}
	}
	return allowed
}

func parseHoufengReference(commentLine, linkLine string, allowed map[string]struct{}) (DocumentRenderNode, error) {
	comment := houfengRefCommentPattern.FindStringSubmatch(strings.TrimSpace(commentLine))
	link := houfengRefLinkPattern.FindStringSubmatch(strings.TrimSpace(linkLine))
	if comment == nil || link == nil || comment[1] != link[2] || comment[2] != link[3] {
		return DocumentRenderNode{}, ErrInvalidDocumentMarkdown
	}
	if _, ok := allowed[comment[1]+"\n"+comment[2]]; !ok {
		return DocumentRenderNode{}, ErrInvalidDocumentMarkdown
	}
	children, err := parseSharedInlines(link[1])
	if err != nil {
		return DocumentRenderNode{}, err
	}
	return DocumentRenderNode{Type: DocumentRenderNodeReference, Kind: comment[1], ID: comment[2], Children: children}, nil
}
