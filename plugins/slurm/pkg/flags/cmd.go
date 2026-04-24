package flags

type Flag struct {
	Full  string
	Short string
}

var (
	JidFlag = Flag{Full: "jid"}
)
