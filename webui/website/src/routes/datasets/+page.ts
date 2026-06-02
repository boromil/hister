const modules = import.meta.glob('../../content/datasets/*.json', { eager: true });

export interface Dataset {
  slug: string;
  name: string;
  description: string;
  downloadUrl: string;
  image: string | null;
  tags: string[];
  license: string;
  author: string;
}

export async function load() {
  const datasets: Dataset[] = Object.entries(modules).map(([path, mod]) => {
    const slug = path.split('/').pop()?.replace('.json', '') ?? path;
    const data = mod as {
      name: string;
      description: string;
      downloadUrl: string;
      image?: string | null;
      tags?: string[];
      license: string;
      author: string;
    };
    return {
      slug,
      name: data.name,
      description: data.description,
      downloadUrl: data.downloadUrl,
      image: data.image ?? null,
      tags: data.tags ?? [],
      license: data.license,
      author: data.author,
    };
  });

  datasets.sort((a, b) => a.name.localeCompare(b.name));

  return { datasets };
}
