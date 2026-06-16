package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"os"

	"golang.org/x/image/draw"
)

func main() {
	srcPath := "../PrismCast_LOGO.png"
	if len(os.Args) > 1 {
		srcPath = os.Args[1]
	}

	srcFile, err := os.Open(srcPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening source: %v\n", err)
		os.Exit(1)
	}
	defer srcFile.Close()

	srcImg, err := png.Decode(srcFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error decoding PNG: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Source image: %dx%d\n", srcImg.Bounds().Dx(), srcImg.Bounds().Dy())

	// Windows ICO format: include all standard sizes plus original high-res
	// For crisp display on all DPI settings (100%, 125%, 150%, 200%, 250%, 400%)
	sizes := []int{16, 20, 24, 32, 40, 48, 64, 96, 128, 256}

	type iconEntry struct {
		width   int
		height  int
		pngData []byte
	}

	var entries []iconEntry

	for _, size := range sizes {
		resized := resizeImage(srcImg, size, size)
		pngData := encodePNG(resized)
		entries = append(entries, iconEntry{width: size, height: size, pngData: pngData})
		fmt.Printf("  %dx%d: %d bytes\n", size, size, len(pngData))
	}

	// Also include the original full-resolution image for maximum quality
	origPNG := encodePNG(srcImg)
	origW := srcImg.Bounds().Dx()
	entries = append(entries, iconEntry{width: origW, height: origW, pngData: origPNG})
	fmt.Printf("  %dx%d (original): %d bytes\n", origW, origW, len(origPNG))

	numImages := uint16(len(entries))
	headerSize := 6
	dirEntrySize := 16
	dirSize := dirEntrySize * len(entries)
	dataOffset := headerSize + dirSize

	offsets := make([]uint32, len(entries))
	currentOffset := uint32(dataOffset)
	for i, entry := range entries {
		offsets[i] = currentOffset
		currentOffset += uint32(len(entry.pngData))
	}

	outPath := "../prismcast/build/windows/icon.ico"
	if len(os.Args) > 2 {
		outPath = os.Args[2]
	}

	outFile, err := os.Create(outPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output: %v\n", err)
		os.Exit(1)
	}
	defer outFile.Close()

	// ICO header: reserved(2), type(2)=1, count(2)
	binary.Write(outFile, binary.LittleEndian, uint16(0))     // Reserved
	binary.Write(outFile, binary.LittleEndian, uint16(1))     // Type: 1 = ICO
	binary.Write(outFile, binary.LittleEndian, numImages)     // Image count

	// Directory entries
	for i, entry := range entries {
		w := uint8(entry.width)
		h := uint8(entry.height)
		if entry.width >= 256 {
			w = 0 // 0 means 256 in ICO format
		}
		if entry.height >= 256 {
			h = 0
		}

		binary.Write(outFile, binary.LittleEndian, w)                        // Width
		binary.Write(outFile, binary.LittleEndian, h)                        // Height
		binary.Write(outFile, binary.LittleEndian, uint8(0))                 // Color palette
		binary.Write(outFile, binary.LittleEndian, uint8(0))                 // Reserved
		binary.Write(outFile, binary.LittleEndian, uint16(1))                // Color planes
		binary.Write(outFile, binary.LittleEndian, uint16(32))               // Bits per pixel (RGBA/PNG)
		binary.Write(outFile, binary.LittleEndian, uint32(len(entry.pngData))) // Data size
		binary.Write(outFile, binary.LittleEndian, offsets[i])               // Data offset
	}

	// Image data
	for _, entry := range entries {
		outFile.Write(entry.pngData)
	}

	fmt.Printf("\nICO file written to %s (%d bytes, %d images)\n", outPath, currentOffset, len(entries))
}

func resizeImage(src image.Image, width, height int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	// Use high-quality Catmull-Rom scaling for sharp results at small sizes
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}
