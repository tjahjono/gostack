# Application migrations

Place application-owned paired timestamp migrations here. This framework repository intentionally has no business application schema. Framework migrations run first. Both directories must exist, even when this stream is empty.

Migration SQL must be transaction-compatible, with no explicit transaction control. Nontransactional operations such as concurrent index creation require a future explicitly reviewed extension and are currently unsupported. Applied migration names and both SQL directions are immutable; checksums cover all three.
