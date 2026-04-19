#!/bin/bash

OUTPUT_FILE="all_commits.txt"

# Clear the file if it already exists
> "$OUTPUT_FILE"

for branch in $(git branch --format='%(refname:short)'); do
    echo "=== $branch ===" >> "$OUTPUT_FILE"
    git log "$branch" --pretty=format:"%h %an <%ae> %ad %s" >> "$OUTPUT_FILE"
    echo -e "\n" >> "$OUTPUT_FILE"
done

echo "Done! Commits saved to $OUTPUT_FILE"