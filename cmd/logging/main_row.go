package main

import (
	"image"
	"strings"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

var (
	mainRowHeight = unit.Dp(280)
	qsoPanelWidth = unit.Dp(720)
)

// mainRow renders the row directly below the status row: full window
// width, fixed height, framed by a 1dp blue border. The row is split
// horizontally into a 2/3-width left panel and a 1/3-width right
// panel using a Flex with weighted (Flexed) children.
func mainRow(theme *material.Theme) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return widget.Border{
			//Color: green600,
			//Width: unit.Dp(2),
		}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			// Set the size of the main row to be the full width of the window and a fixed height.
			gtx.Constraints.Max.X = gtx.Dp(windowWidth)
			gtx.Constraints.Max.Y = gtx.Dp(mainRowHeight)
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			gtx.Constraints.Min.Y = gtx.Constraints.Max.Y
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(qsoPanel(theme)))
		})
	}
}

func qsoPanel(theme *material.Theme) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return widget.Border{
			//Color: indigo400,
			//Width: unit.Dp(2),
		}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{
				Axis: layout.Vertical,
			}.Layout(gtx,
				layout.Rigid(qsoSectionTop(theme)),
				layout.Rigid(qsoSectionMiddle),
				layout.Rigid(qsoSectionBottom))
		})
	}
}

//func qsoSectionTop(theme *material.Theme) layout.Widget {
//	return func(gtx layout.Context) layout.Dimensions {
//		return widget.Border{
//			Color: indigo400,
//			Width: unit.Dp(2),
//		}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
//			ip := image.Pt(gtx.Dp(qsoPanelWidth), gtx.Dp(90))
//			return layout.Dimensions{Size: ip}
//		})
//	}
//}

func qsoSectionMiddle(gtx layout.Context) layout.Dimensions {
	return widget.Border{
		Color: indigo400,
		Width: unit.Dp(2),
	}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		ip := image.Pt(gtx.Dp(qsoPanelWidth), gtx.Dp(90))
		return layout.Dimensions{Size: ip}
	})
}

func qsoSectionBottom(gtx layout.Context) layout.Dimensions {
	return widget.Border{
		Color: indigo400,
		Width: unit.Dp(2),
	}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		ip := image.Pt(gtx.Dp(qsoPanelWidth), gtx.Dp(90))
		return layout.Dimensions{Size: ip}
	})
}

var (
	callsignEditor widget.Editor
	rstSentEditor  widget.Editor
	rstRcvdEditor  widget.Editor
	modeState      modeCycle
)

func labeledMode(theme *material.Theme) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Start}.Layout(gtx,
			layout.Rigid(material.Body2(theme, "Mode").Layout),
			layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return modeState.Layout(gtx, theme)
			}),
		)
	}
}

func qsoSectionTop(theme *material.Theme) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		// Force callsign to uppercase. SetText is a no-op when text matches,
		// and the guard avoids resetting the caret when there's nothing to do.
		if t := callsignEditor.Text(); t != strings.ToUpper(t) {
			start, end := callsignEditor.Selection()
			callsignEditor.SetText(strings.ToUpper(t))
			callsignEditor.SetCaret(start, end)
		}

		return widget.Border{
			Color: indigo400,
			Width: unit.Dp(2),
		}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start}.Layout(gtx,
					layout.Rigid(labeledInput(theme, "Callsign", &callsignEditor, "", callsignFieldWidth)),
					layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
					layout.Rigid(labeledInput(theme, "RST Sent", &rstSentEditor, "59", rstFieldWidth)),
					layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
					layout.Rigid(labeledInput(theme, "RST Rcvd", &rstRcvdEditor, "59", rstFieldWidth)),
					layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
					layout.Rigid(labeledMode(theme)),
				)
			})
		})
	}
}
