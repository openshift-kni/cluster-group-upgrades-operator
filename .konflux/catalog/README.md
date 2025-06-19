# catalog.yaml

The catalog.yaml file was intentionally removed from the git repository because tt would be confusing to leave a static FBC here when we exclusively generate them at runtime to include them in the FBC container image.

However since the file must exist for the Makefile target to generate the bundle the Makefile was updated to create the file when necessary.

You can still run the target manually to generate the catalog.yaml file but its contents should not be committed, and it has been added to `.gitignore`.
