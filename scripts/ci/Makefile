ifeq ($(GITHUB_ACTIONS),true)
	CONTAINER_TERMINAL := -i
else
	CONTAINER_TERMINAL := -it
endif

export CONTAINER_TERMINAL

benchmark:
	./benchmark-and-push.sh || (echo "Benchmark and push failed" && exit 1)

smoke:
	./smoke-test.sh || (echo "Smoke test failed" && exit 1)

regression:
	./regression-test.sh || (echo "Regression test failed" && exit 1)

correctness:
	./correctness-test.sh || (echo "Correctness test failed" && exit 1)

setup-full:
	./setup-full.sh || (echo "Setup full failed" && exit 1)

setup-build:
	./setup-build.sh || (echo "Setup build failed" && exit 1)
