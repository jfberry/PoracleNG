package db

// LoadOverrideKeys exposes loadOverrideKeys to the external db_test package,
// whose matcher-level tests cannot live in package db (matching imports db).
var LoadOverrideKeys = loadOverrideKeys
