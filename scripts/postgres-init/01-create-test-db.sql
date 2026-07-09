-- Runs once on first postgres init (docker-entrypoint-initdb.d). Creates the
-- separate database the Go test suites expect in DATABASE_URL_TEST, so
-- `docker compose up -d postgres` alone is enough to run pgstore + e2e tests.
CREATE DATABASE fleet_test;
