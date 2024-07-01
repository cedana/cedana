ifeq ($(GITHUB_ACTIONS),true)
	CONTAINER_TERMINAL := -i
else
	CONTAINER_TERMINAL := -it
endif

export CONTAINER_TERMINAL

benchmark:
	./benchmark-and-push.sh

smoke:
	./smoke-test.sh

regression:
	./regression-test.sh

setup:
	./setup.sh
