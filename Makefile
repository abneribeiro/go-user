include .env
export


run: 
	go run cmd/api/main.go

db-up:
	docker compose up -d db

db-down:
	docker compose down