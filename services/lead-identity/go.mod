module github.com/lead/services/lead-identity

go 1.24.0

require (
	github.com/go-chi/chi/v5 v5.0.12
	github.com/google/uuid v1.6.0
	github.com/nyaruka/phonenumbers v1.3.6
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20221227161230-091c0ba34f0a // indirect
	github.com/jackc/puddle/v2 v2.2.1 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
)

require (
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/jackc/pgx/v5 v5.5.5 // indirect
	github.com/lead/libs/go v0.0.0
	golang.org/x/text v0.34.0 // indirect
	google.golang.org/protobuf v1.36.9 // indirect
)

replace github.com/lead/libs/go => ../../libs/go
