package alpha

import "gosvm/examples/global-values/log"

// A second file: its variables and init function come after alpha.go's.
var A2 = log.Mark('2') + A1

func init() {
	log.Mark('D')
	Bound(3) // changes S through the bound pointer
}
