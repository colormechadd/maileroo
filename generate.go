package mailaroo

// Generate Templ components
//go:generate templ generate

// Build Tailwind CSS
//go:generate tailwindcss -i ./static/css/input.css -c ./tailwind.config.js -o ./static/css/output.css --minify
