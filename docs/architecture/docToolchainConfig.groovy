inputFiles = [
    [file: 'arc42/arc42.adoc', formats: ['html','pdf']]
]

outputPath = 'build/docs'

// Keep diagram rendering local to Asciidoctor Diagram / PlantUML.
// No project source-code artifact is required to generate these architecture docs.
