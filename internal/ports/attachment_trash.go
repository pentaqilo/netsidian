package ports

// AttachmentTrashStore keeps original bytes and a persistent restore record.
// Entries share the folder-trash catalog, with Kind="attachment".
type AttachmentTrashStore interface {
	StashAttachment(spaceID, path, trashID string) error
}
