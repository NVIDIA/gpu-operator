BUNDLES := $(patsubst %,gpu-operator-bundle-%.zip,$(wildcard [1-9]*))
CRDS := $(wildcard ../../crds/*.yaml)

.PHONY : bundles clean
bundles :: $(BUNDLES)
clean :: ; $(RM) $(BUNDLES)

gpu-operator-bundle-%.zip : %
	sed -e 's/OPERATOR_VERSION/gpu-operator.v'$*'/' gpu-operator.package.yaml > $*/gpu-operator.package.yaml
	zip -rj9 $@ $< $(CRDS)
