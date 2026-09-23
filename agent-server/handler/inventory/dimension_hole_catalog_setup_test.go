package inventory

import (
	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/testfixture"
)

func init() {
	if err := model.InstallDimensionHoleCatalog(testfixture.DimensionHoles()); err != nil {
		panic(err)
	}
}
