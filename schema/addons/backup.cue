package addons

#SysdefBackup: {
	backup!: [string]: #SysdefBackupEntry
}

// backup of one application
#SysdefBackupEntry: {
	// volume name -> paths inside the volume
	volumes!: [VolumeName=string]: [...string]
	// commands the backup system runs around a snapshot
	snapshotHooks!: #SysdefSnapshotHooks
	// commands the backup system runs around a restore
	restoreHooks!: #SysdefRestoreHooks
}

#SysdefSnapshotHooks: {
	// commands run before the snapshot
	pre!: [...string]
	// commands run after the snapshot
	// failed
	post!: [...string]
}

#SysdefRestoreHooks: {
	// shell script run in the backup container while the application is stopped,
	// after its files and database are restored, e.g. to put a restored file in
	// place
	postRestoreScript?: string
	// commands run while the application is stopped, after its files and
	// database are restored, e.g. to empty its cache
	offline!: [...string]
	// commands run after the application is started again
	online!: [...string]
}

#SysdefBackupDefaults: {
	backup: {}
	backup: [string]: snapshotHooks: pre: *[] | [...string]
	backup: [string]: snapshotHooks: post: *[] | [...string]
	backup: [string]: restoreHooks: offline: *[] | [...string]
	backup: [string]: restoreHooks: online: *[] | [...string]
}
