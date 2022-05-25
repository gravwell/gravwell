# if this fails to build, you may need to install the pandoc xnos suite:
# pip install pandoc-fignos pandoc-eqnos pandoc-tablenos pandoc-secnos --user

pdf: migrate.md
	pandoc migrate.md --number-sections --filter pandoc-xnos -s -o migrate.pdf
