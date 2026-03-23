package flags

type Flag struct {
	Full  string
	Short string
}

var (
	PathFlag = Flag{Full: "path", Short: "p"}
	JidFlag  = Flag{Full: "jid", Short: "j"}
)
