FROM golang:1.26-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /out/roundtable-api ./api/cmd/server

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install --no-install-recommends -y ca-certificates git \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /out/roundtable-api /usr/local/bin/roundtable-api
RUN mkdir -p /app/.roundtable
EXPOSE 8080
ENTRYPOINT ["roundtable-api"]
CMD ["-addr", "0.0.0.0:8080", "-workspace-root", "/workspace", "-database", "/app/.roundtable/roundtable.db"]
