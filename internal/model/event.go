package model

import "time"

// UpdateEvent — ядро модели данных системы.
type UpdateEvent struct {
	SourceURL   string    // URL записи (link) либо URL фида
	RawContent  string    // нормализованный контент записи
	PublishedAt time.Time // UTC
	Fingerprint string    // заполняется deduplicator'ом
}
