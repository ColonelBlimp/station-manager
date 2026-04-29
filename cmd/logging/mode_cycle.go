package main

import (
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

var modes = []string{"USB", "LSB", "CW"}

const modeFieldWidth = unit.Dp(60)

type modeCycle struct {
	selected int
	btn      widget.Clickable
}

func (m *modeCycle) Selected() string { return modes[m.selected] }

func (m *modeCycle) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if m.btn.Clicked(gtx) {
		m.selected = (m.selected + 1) % len(modes)
	}
	return m.btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Dp(modeFieldWidth)
		gtx.Constraints.Max.X = gtx.Dp(modeFieldWidth)
		return widget.Border{
			Color:        gray500,
			Width:        unit.Dp(1),
			CornerRadius: unit.Dp(4),
		}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(6)).Layout(gtx,
				material.Body1(th, m.Selected()).Layout)
		})
	})
}
