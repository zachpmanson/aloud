An interactive wrapper around macOS `say` for reading articles.

Works very well with [readable](https://gitlab.com/gardenappl/readability-cli). Can be piped like so:

```bash
readable "https://www.npr.org/2026/04/21/g-s1-118178/japan-ban-lethal-weapons-exports" -o article.html && textutil -convert txt article.html && cat article.txt | aloud
```

Even better oneliner that saves the article based on the first sentence.

```bash
readable "https://www.npr.org/2026/04/21/g-s1-118178/japan-ban-lethal-weapons-exports" -o article.html > /dev/null 2>&1 && \
textutil -convert txt article.html > /dev/null 2>&1 && \
title=$(head -1 article.txt | tr -cd '[:alnum:] ._-' | cut -c1-255) && \
mv article.txt "${title}.md" && \
cat "${title}.md" | aloud
```
