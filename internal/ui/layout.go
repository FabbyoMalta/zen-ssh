package ui

type layoutVariant int

const (
	layoutCompact layoutVariant = iota
	layoutStacked
	layoutSplit
)

type layoutState struct {
	variant       layoutVariant
	contentWidth  int
	contentHeight int
	listWidth     int
	detailWidth   int
	listHeight    int
	detailHeight  int
}

func calculateLayout(width, height int) layoutState {
	contentWidth := maxInt(30, width-4)
	contentHeight := maxInt(8, height-7)
	state := layoutState{variant: layoutCompact, contentWidth: contentWidth, contentHeight: contentHeight, listWidth: contentWidth, listHeight: contentHeight}
	switch {
	case width >= 100:
		state.variant = layoutSplit
		state.listWidth = maxInt(52, contentWidth*58/100)
		state.detailWidth = maxInt(34, contentWidth-state.listWidth-1)
		state.detailHeight = contentHeight
	case width >= 60:
		state.variant = layoutStacked
		state.listHeight = maxInt(6, contentHeight*55/100)
		state.detailWidth = contentWidth
		state.detailHeight = maxInt(5, contentHeight-state.listHeight-1)
	}
	return state
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
