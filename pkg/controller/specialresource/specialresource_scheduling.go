package specialresource

func addPriorityPreemptionControls(n *SRO) error {

	res := Resources{}
	ctrl := controlFunc{}
	ctrl = append(ctrl, PriorityClass)

	for _, prio := range n.ins.Spec.PriorityClassItems {
		res.PriorityClass = prio
		n.controls = append(n.controls, ctrl)
		n.resources = append(n.resources, res)
	}

	return nil
}

func addTaintsTolerationsControls(n *SRO) error {
	res := Resources{}
	ctrl := controlFunc{}
	ctrl = append(ctrl, Taint)

	for _, taint := range n.ins.Spec.Taints {
		res.Taint = taint
		n.controls = append(n.controls, ctrl)
		n.resources = append(n.resources, res)
	}

	return nil

}
