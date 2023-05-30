
.PHONY: all
all:
	go build -ldflags="-s -w" .
	docker build . -t langbeck/rollouts-watcher
	docker push langbeck/rollouts-watcher
