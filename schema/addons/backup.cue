package addons

#SysdefBackup: {
	backup!: [string]: #SysdefBackupEntry
}

#SysdefBackupEntry: {
	// VolumeName -> Target Directory
	[VolumeName=string]: [...string]
}

#SysdefBackupDefaults: {}
