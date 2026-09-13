package model

// IsTownRegionV188 reports the non-battlefield town regions in the maintained
// vSRO 1.188 REGION_REFERENCE catalog. Job clothes may only be put on or
// removed while the character remains in one of these regions.
func IsTownRegionV188(regionID int16) bool {
	switch regionID {
	case 22830, 22831, 22832, 22833, 23085, 23086, 23087, 23088, 23089,
		23341, 23342, 23343, 23344, 23345, 23346, 23347, 23598, 23599,
		23600, 23601, 23602, 23603, 23854, 23855, 23856, 23857, 24110,
		24111, 24112, 24113, 24367, 24368, 24369, 24623, 24624, 24625,
		23431, 23432, 23686, 23687, 23688, 23943, 24199, 24744, 24758,
		24999, 25000, 25001, 25255, 25256, 25257, 26264, 26265, 26520,
		26521, 26445, 27473, 26446, 26447, 26448, 26701, 26702, 26703,
		26704, 26957, 26958, 26959, 26960, 26961, 27213, 27214, 27215,
		27216, 27217, 27469, 27470, 27471, 27472, 27243, 27244, 27499,
		27500:
		return true
	default:
		return false
	}
}
