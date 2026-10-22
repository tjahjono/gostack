CREATE TABLE public.gostack_migration_checksums (
    stream text NOT NULL CHECK (stream IN ('framework', 'app')),
    version bigint NOT NULL CHECK (version > 0),
    checksum text NOT NULL CHECK (checksum ~ '^[0-9a-f]{64}$'),
    installed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (stream, version)
);
REVOKE ALL ON public.gostack_migration_checksums FROM PUBLIC;
