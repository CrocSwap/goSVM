module example.com/shared-service

go 1.22

require example.com/shared-app v0.0.0

// Local development stand-in for the unpublished shared application module.
replace example.com/shared-app => ..
