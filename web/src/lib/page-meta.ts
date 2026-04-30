import { useEffect } from "react";

type PageMetaInput = {
  title: string;
  description: string;
  canonicalPath: string;
};

function upsertMeta(name: string, content: string) {
  let element = document.head.querySelector(`meta[name="${name}"]`) as HTMLMetaElement | null;
  if (!element) {
    element = document.createElement("meta");
    element.setAttribute("name", name);
    document.head.appendChild(element);
  }
  element.setAttribute("content", content);
}

function upsertCanonical(href: string) {
  let element = document.head.querySelector('link[rel="canonical"]') as HTMLLinkElement | null;
  if (!element) {
    element = document.createElement("link");
    element.setAttribute("rel", "canonical");
    document.head.appendChild(element);
  }
  element.setAttribute("href", href);
}

export function usePageMeta(input: PageMetaInput) {
  useEffect(() => {
    document.title = input.title;
    upsertMeta("description", input.description);
    upsertMeta("robots", "index,follow");
    upsertCanonical(new URL(input.canonicalPath, window.location.origin).toString());
  }, [input.canonicalPath, input.description, input.title]);
}
