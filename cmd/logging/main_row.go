package main

import (
	"gioui.org/layout"
	"gioui.org/widget"
)

// mainRow renders the row directly below the status row: full window
// width, fixed height, framed by a 1dp blue border. The row is split
// horizontally into a 2/3-width left panel and a 1/3-width right
// panel using a Flex with weighted (Flexed) children.
func mainRow() layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		gtx.Constraints.Min.Y = gtx.Dp(mainRowHeight)
		gtx.Constraints.Max.Y = gtx.Dp(mainRowHeight)

		return widget.Border{
			//Color: mainRowBorderColor,
			//Width: unit.Dp(1),
		}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Flexed(2, leftPanel()),
				layout.Flexed(1, borderedPanel(rightPanelBorderColor)),
			)
		})
	}
}

func leftPanel() layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = gtx.Constraints.Max
		return widget.Border{}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{}
		})
	}
}
