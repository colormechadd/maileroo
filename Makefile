.PHONY: build run test clean migrate-up migrate-down templ

build: generate
	go build -o mailaroo cmd/mailaroo/*.go

run: build
	./mailaroo serve

test:
	go test ./...

clean:
	rm -f mailaroo static/css/output.css
	find . -name "*_templ.go" -delete

generate:
	go generate ./...

docker-build:
	docker build -t mailaroo .

tailwind-watch:
	tailwindcss -c ./tailwind.config.js -i ./static/css/input.css -o ./static/css/output.css --watch

# Placeholder for migrations (if using a tool like golang-migrate)
migrate-up:
	@echo "Running migrations..."
	dbmate up

migrate-down:
	@echo "Rolling back migrations..."
	dbmate down